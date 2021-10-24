package http

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

var (
	crlf = []byte{'\r', '\n'}
	// Headers are completed when we have CRLF twice
	headerTerminator          = append(crlf, crlf...)
	contentLengthHeader       = []byte("Content-Length: ")
	contentLengthHeaderLength = len(contentLengthHeader)
	errBadRequest             = errors.New("bad request")
)

// isRequestComplete is used to determine if the entire request has been read into the data stream.
// If the entire request has been read, we return true, and if there is still data to be read, we
// return false. An error is returned if the request is malformed, or if the request is streaming data
// using the Transfer-Encoding: chunked encoding, which we are not supporting as of this time.
func IsRequestComplete(data []byte) (bool, error) {
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

func parseContentLength(clen []byte) (int64, error) {
	// If we are lower than 0 or greater than 9, then we aren't an integer.
	if clen[0] < '0' || clen[0] > '9' {
		return -1, errBadRequest
	}

	// Start at the highest order of magnitude
	zeroes := len(clen) - 1
	length := int64(0)
	for i := 0; i < len(clen); i++ {
		// Error possibilities
		if clen[i] < '0' || clen[i] > '9' {
			return -1, errBadRequest
		}
		// v := byteToIntJump(clen[i])
		v := byteToIntSlice[clen[i]]

		// Error possibilities
		if v < 0 {
			return -1, errBadRequest
		}

		// Add the magnitude to the length
		if zeroes == 0 {
			length += v
		} else {
			// The Pow10 can probably be done with a simple lookup table
			// since 99% of the time we will probably be within 5 zeroes.
			length += v * int64(math.Pow10(zeroes))
		}
		zeroes--
	}

	return length, nil
}

var byteToIntSlice = []int64{
	'0': 0,
	'1': 1,
	'2': 2,
	'3': 3,
	'4': 4,
	'5': 5,
	'6': 6,
	'7': 7,
	'8': 8,
	'9': 9,
}

func byteToIntJump(b byte) int64 {
	switch b {
	case '0':
		return 0
	case '1':
		return 1
	case '2':
		return 2
	case '3':
		return 3
	case '4':
		return 4
	case '5':
		return 5
	case '6':
		return 6
	case '7':
		return 7
	case '8':
		return 8
	case '9':
		return 9
	default:
		return -1
	}
}
