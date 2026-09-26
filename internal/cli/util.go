package cli

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// confirm asks before something irreversible.
//
// assumeYes comes from --yes and skips the question outright.
//
// The question is asked on /dev/tty rather than standard input, which is how sudo and
// ssh do it: a prompt on stdin is unanswerable the moment anything is piped in, and
// stdin's file mode cannot distinguish a terminal from /dev/null — both are character
// devices. If there is no controlling terminal this refuses rather than guessing:
// assuming yes would destroy data on a stray invocation, and assuming no would make
// the command useless in automation. --yes is the explicit way to say it.
func confirm(prompt string, assumeYes bool) bool {
	if assumeYes {
		return true
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"%s\nRefusing: there is no terminal to ask on. Pass --yes to confirm.\n", prompt)
		return false
	}
	defer tty.Close()

	fmt.Fprintf(tty, "%s [y/N] ", prompt)
	answer, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// isPEM reports whether a file is textual PEM rather than a binary archive.
func isPEM(raw []byte) bool {
	return strings.Contains(string(raw), "-----BEGIN")
}

func encodeBase64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func decodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(value))
}
