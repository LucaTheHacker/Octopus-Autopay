package payment

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Confirm asks a yes/no question on stderr/stdin. Defaults to no on EOF or any
// non-affirmative answer.
func Confirm(reader *bufio.Reader, msg string) bool {
	fmt.Fprint(os.Stderr, msg)
	s, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "s", "si", "sì":
		return true
	}
	return false
}
