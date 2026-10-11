package core

// PieceUnit is what a PieceMap counts in.
type PieceUnit string

const (
	// UnitBytes is a file fetched over HTTP, in bytes.
	UnitBytes PieceUnit = "bytes"
	// UnitArticles is a file fetched from Usenet, in its articles.
	UnitArticles PieceUnit = "articles"
	// UnitBlocks is a file under a par2 check, in the blocks of its set.
	UnitBlocks PieceUnit = "blocks"
)

// PieceState is what one stretch of a file is.
type PieceState string

const (
	PieceMissing PieceState = "missing"
	PieceDone    PieceState = "done"
	// PieceLost is a stretch that arrived damaged or not at all: an article
	// no server has, or a block the par2 check found bad.
	PieceLost PieceState = "lost"
)

// PieceRun is Len units in a row that share a state.
type PieceRun struct {
	State PieceState `json:"state"`
	Len   int64      `json:"len"`
}

// PieceMap is which parts of one file are here: its Total units in file order
// as runs, and the unit each open connection is writing at as Heads.
type PieceMap struct {
	Unit  PieceUnit  `json:"unit"`
	Total int64      `json:"total"`
	Runs  []PieceRun `json:"runs"`
	Heads []int64    `json:"heads,omitempty"`
}

// PieceRuns turns the state of every unit of a file into runs.
func PieceRuns(n int, state func(i int) PieceState) []PieceRun {
	var runs []PieceRun
	for i := range n {
		s := state(i)
		if k := len(runs); k > 0 && runs[k-1].State == s {
			runs[k-1].Len++
			continue
		}
		runs = append(runs, PieceRun{State: s, Len: 1})
	}
	return runs
}
