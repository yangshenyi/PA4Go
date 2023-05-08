package pa

import "fmt"

func (a *analysis) solve() {
	if a.log != nil {
		fmt.Fprintf(a.log, "\n\n=== Solving ====\n\n")
	}
}
