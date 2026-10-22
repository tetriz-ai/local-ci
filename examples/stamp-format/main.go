// Command stamp-format shows the shape of the stamp local-ci writes on a commit and a
// merge gate reads back. local-ci stores a projectinfo.StampInfo as base64-encoded
// JSON in a git note; this program builds one, encodes it the same way, and
// decodes it again, so you can see exactly what a gate inspects to decide whether
// a commit's checks already passed. No git or GitHub access is needed.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func main() {
	stamp := projectinfo.StampInfo{
		CommitSHA:    "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b",
		BaseBranch:   "main",
		Version:      "v0.1.0",
		ChecksStatus: map[string]bool{"proto_breaking": true, "govulncheck": true},
	}

	raw, err := json.Marshal(stamp)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	fmt.Printf("git note (base64): %s\n", encoded)

	var decoded projectinfo.StampInfo
	rawBack, _ := base64.StdEncoding.DecodeString(encoded)
	_ = json.Unmarshal(rawBack, &decoded)
	fmt.Printf("commit:      %s\n", decoded.CommitSHA)
	fmt.Printf("base branch: %s\n", decoded.BaseBranch)
	fmt.Printf("proto_breaking passed: %t\n", decoded.ChecksStatus["proto_breaking"])
	fmt.Printf("govulncheck passed:    %t\n", decoded.ChecksStatus["govulncheck"])
}
