package parse

import "bytes"

// HasMergeConflicts checks for merge conflict markers in content.
// Returns true if the content contains <<<<<<<, =======, and >>>>>>> markers.
func HasMergeConflicts(content []byte) bool {
	return bytes.Contains(content, []byte("<<<<<<<")) &&
		bytes.Contains(content, []byte("=======")) &&
		bytes.Contains(content, []byte(">>>>>>>"))
}
