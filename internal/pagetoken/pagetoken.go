// Package pagetoken encodes CLI-owned opaque pagination offsets.
package pagetoken

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

const maxEncodedLength = 128

var errInvalid = errors.New("invalid page token")

// Encode binds an offset to one command scope.
func Encode(scope string, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v1:" + scope + ":" + strconv.Itoa(offset)))
}

// Decode validates a token for one command scope and offset ceiling.
func Decode(scope, value string, maxOffset int) (int, error) {
	if value == "" {
		return 0, nil
	}
	if len(value) > maxEncodedLength {
		return 0, errInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	prefix := "v1:" + scope + ":"
	if err != nil || !strings.HasPrefix(string(decoded), prefix) {
		return 0, errInvalid
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(decoded), prefix))
	if err != nil || offset < 0 || offset > maxOffset {
		return 0, errInvalid
	}
	return offset, nil
}
