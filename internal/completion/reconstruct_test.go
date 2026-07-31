package completion

import "testing"

func TestReconstructLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		pos    int
		result Result
		item   Item
		want   string
	}{
		{name: "path prefix", line: "cat src/ma", pos: len("cat src/ma"), result: Result{Prefix: "src/"}, item: Item{Value: "main.go"}, want: "cat src/main.go"},
		{name: "quoted word", line: `echo "hel`, pos: len(`echo "hel`), result: Result{Prefix: `"`}, item: Item{Value: `hello"`}, want: `echo "hello"`},
		{name: "replacement", line: "git chek", pos: len("git chek"), item: Item{Value: "checkout"}, want: "git checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, ok := ReconstructLine(tt.line, tt.pos, tt.result, tt.item)
			if !ok || got != tt.want {
				t.Errorf("ReconstructLine() = %q, %v; want %q, true", got, ok, tt.want)
			}
		})
	}
}
