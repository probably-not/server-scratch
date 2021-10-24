package evio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/tidwall/evio"
)

func NewHandler(ctx context.Context, loops, port int) evio.Events {
	var handler evio.Events
	handler.NumLoops = loops
	handler.LoadBalance = evio.RoundRobin

	// Serving fires on server up (one time)
	handler.Serving = func(server evio.Server) evio.Action {
		fmt.Println("evio server started with", server.NumLoops, "event loops on port", port)

		select {
		case <-ctx.Done():
			return evio.Shutdown
		default:
			return evio.None
		}
	}

	// Opened fires on opening new connections (per connection)
	handler.Opened = func(c evio.Conn) ([]byte, evio.Options, evio.Action) {
		c.SetContext(&evio.InputStream{})

		select {
		case <-ctx.Done():
			return nil, evio.Options{}, evio.Close
		default:
			return nil, evio.Options{}, evio.None
		}
	}

	// Closed fires on closing connections (per connection)
	handler.Closed = func(c evio.Conn, err error) evio.Action {
		if err != nil {
			fmt.Println("connection between", c.LocalAddr(), "and", c.RemoteAddr(), "has been closed with error value", err)
		}

		select {
		case <-ctx.Done():
			return evio.Shutdown
		default:
			return evio.None
		}
	}

	// Data fires on data being sent to a connection (per connection, per data frame read)
	handler.Data = func(c evio.Conn, in []byte) ([]byte, evio.Action) {
		if len(in) == 0 {
			return nil, evio.None
		}

		stream := c.Context().(*evio.InputStream)
		data := stream.Begin(in)

		complete, err := isRequestComplete(data)
		if err != nil {
			fmt.Println("Uh oh, there was an error checking completeness?", err)
			return nil, evio.Close
		}

		stream.End(data)
		if !complete {
			return nil, evio.None
		}

		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
		if err != nil {
			fmt.Println("Uh oh, there was an error creating the request?", err)
			return nil, evio.Close
		}

		body, err := readall(req.Body)
		if err != nil {
			fmt.Println("Uh oh, there was an error reading the request body?", err)
			return nil, evio.Close
		}

		res := http.Response{
			StatusCode:    200,
			ProtoMajor:    1,
			ProtoMinor:    1,
			ContentLength: int64(len(body)),
			Close:         false,
			Body:          closer(bytes.NewReader(body)),
		}
		buf := bytes.NewBuffer(nil)
		err = res.Write(buf)
		if err != nil {
			fmt.Println("Uh oh, there was an error writing the response?", err)
			return nil, evio.Close
		}

		select {
		case <-ctx.Done():
			return nil, evio.Close
		default:
			// Reset the connection context to an empty input stream once we have completed a full request in order to
			// ensure that the next request starts empty.
			c.SetContext(&evio.InputStream{})
			return buf.Bytes(), evio.None
		}
	}

	handler.Tick = func() (delay time.Duration, action evio.Action) {
		select {
		case <-ctx.Done():
			return time.Second, evio.Shutdown
		default:
			return time.Second, evio.None
		}
	}

	return handler
}

var (
	crlf = []byte{'\r', '\n'}
	// Headers are completed when we have CRLF twice
	headerTerminator          = append(crlf, crlf...)
	contentLengthHeader       = []byte("Content-Length: ")
	contentLengthHeaderLength = len(contentLengthHeader)
	errBadRequest             = errors.New("bad request")
)

func isRequestComplete(data []byte) (bool, error) {
	// If we haven't gotten to the header terminator, then the request hasn't been fully read yet
	htIdx := bytes.Index(data, headerTerminator)
	if htIdx < 0 {
		return false, nil
	}
	htEndIdx := htIdx + 4

	clIdx := bytes.Index(data, contentLengthHeader)
	if clIdx < 0 {
		// If the end of the header terminator is equal to the length of the data,
		// then this request has no body, and is complete.
		if htEndIdx == len(data) {
			return true, nil
		}

		// If we have not received a Content-Length Header in all of the headers, and there is a body, this is a bad request.
		// We don't accept Transfer-Encoding: chunked for now, and Content-Length is required for when there is a body.
		return false, errBadRequest
	}

	clEndIdx := bytes.Index(data[clIdx:], crlf)
	// If for some reason we don't have the line terminator in the data then this is a problem...
	if clEndIdx < 0 {
		return false, errBadRequest
	}
	clEndIdx += clIdx

	// If the end of the header terminator is equal to the length of the data,
	// then this request has no body yet, so we wait for the entire body to arrive.
	if htEndIdx >= len(data) {
		return false, nil
	}

	// Get the Content-Length value as an integer
	clenbytes := data[clIdx+contentLengthHeaderLength : clEndIdx]
	clen, err := strconv.ParseInt(string(clenbytes), 10, 64)
	if err != nil {
		return false, err
	}

	// If the data after the header terminator ending index is less than the Content-Length value, then we are not done reading yet.
	if len(data)-htEndIdx < int(clen) {
		return false, nil
	}

	return true, nil
}