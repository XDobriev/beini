// password-hash reads a password from stdin; never pass it as a CLI argument.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"beini-backend/internal/access"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1027))
	if err != nil {
		fmt.Fprintln(os.Stderr, "не удалось прочитать stdin")
		os.Exit(1)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	hash, err := access.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
