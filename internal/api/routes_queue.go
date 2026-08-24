package api

// The queue: the master switch, the stop mark, the hard stop, and everything
// that changes where a selection sits in the wait order.
//
// Every route here that acts on tasks takes the same selection shape — ids, a
// package name, or all of them — rather than one id in the path. The interface
// acts on a selection, and a route per row turns a hundred-row selection into a
// hundred requests, a hundred store writes and a hundred broadcasts.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// requireSelection refuses a request that names nothing to act on.
//
// "All of them" has to be asked for explicitly, which is why an empty body is
// not read as it: a client that meant to send a selection and sent none is a
// bug, and re-ordering the entire queue is the worst available way to report
// it. The routes that legitimately mean "everything" (start, restart) live in
// routes_tasks.go and say so in their summary.
func requireSelection(w http.ResponseWriter, sel app.Selection) bool {
	if len(sel.Ids) > 0 || sel.Package != nil || sel.All {
		return true
	}
	http.Error(w, "this needs task ids, a package, or all:true", http.StatusBadRequest)
	return false
}

// stopResult is what the hard stop answers with: the transfers it stopped and
// the switch it left behind, so the interface does not have to ask twice to
// find out whether the queue is now halted.
type stopResult struct {
	Ids   []string       `json:"ids"`
	Count int            `json:"count"`
	Queue app.QueueState `json:"queue"`
}

func registerQueue(reg *Registry, a *app.App) {
	// Separate from pausing a task: this decides whether anything new starts at
	// all.
	reg.Add(http.MethodGet, "/api/queue", "the master switch, the stop mark, and how many downloads are in flight",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Queue())
		})
	reg.Add(http.MethodPost, "/api/queue", `halt or release the queue, and arm "finish this one, then stop"`,
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Halted   *bool   `json:"halted,omitempty"`
				StopMark *string `json:"stopMark,omitempty"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Halted != nil {
				a.SetHalted(*body.Halted)
			}
			if body.StopMark != nil {
				a.SetStopMark(*body.StopMark)
			}
			writeJSON(w, a.Queue())
		})

	// The hard stop is two routes because the warning is half the feature. The
	// GET works out what stopping would throw away and takes nothing; the POST
	// stops. A single route that did both would leave the interface guessing at
	// the cost, and a guess that overstates itself once is a dialog nobody reads
	// again.
	reg.Add(http.MethodGet, "/api/queue/stop", "what stopping every transfer right now would throw away",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.StopCost())
		})
	reg.Add(http.MethodPost, "/api/queue/stop", "stop every transfer in flight now, and halt the queue behind them",
		func(w http.ResponseWriter, r *http.Request) {
			ids := a.StopAll()
			writeJSON(w, stopResult{Ids: ids, Count: len(ids), Queue: a.Queue()})
		})

	reg.Add(http.MethodGet, "/api/queue/counters", "files owed, bytes left and the aggregate ETA",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Counters())
		})

	// Served rather than compiled into the interface, for the same reason the
	// cleanup classes are: a menu built from the client's own list offers
	// whatever that build was compiled with, and an entry the server does not
	// implement is a control that does nothing when it is pressed.
	reg.Add(http.MethodGet, "/api/queue/priorities", "the seven priorities the queue orders by, highest first",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, app.Priorities())
		})
	reg.Add(http.MethodPost, "/api/queue/priority", "put a selection, or a whole package, at one of the seven priorities",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				app.Selection
				Priority int `json:"priority"`
			}
			if !decodeJSON(w, r, &body) || !requireSelection(w, body.Selection) {
				return
			}
			bulkDone(w, a.SetPriorityIn(body.Selection, body.Priority))
		})

	reg.Add(http.MethodPost, "/api/queue/move", `move a selection, or a whole package, "top" / "up" / "down" / "bottom" in the wait order`,
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				app.Selection
				Where string `json:"where"`
			}
			if !decodeJSON(w, r, &body) || !requireSelection(w, body.Selection) {
				return
			}
			// Refused by name, because MoveIn answers an unknown direction with an
			// empty list and that is indistinguishable from "nothing was movable".
			switch body.Where {
			case app.MoveTop, app.MoveUp, app.MoveDown, app.MoveBottom:
			default:
				http.Error(w, "where has to be one of top, up, down, bottom", http.StatusBadRequest)
				return
			}
			bulkDone(w, a.MoveIn(body.Selection, body.Where))
		})

	// The drag-and-drop reorder: not a step but a whole new order for one band,
	// so it takes a plain id list rather than app.Selection - a drag already
	// knows exactly which rows moved and where, and it is the interface's own
	// contract for the drag surface, not a case of "id, package, or all". It
	// lives here rather than in routes_tasks.go because it is the same manual
	// order every other route in this file orders, only driven by a complete
	// list instead of a relative step.
	reg.Add(http.MethodPost, "/api/tasks/reorder", "put one whole band of the wait queue in the exact order given, as a drag would",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			ids, err := a.ReorderBand(body.Ids)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			bulkDone(w, ids)
		})

	reg.Add(http.MethodPost, "/api/queue/force", "start a selection now: to the front of the queue, switched on and released",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				app.Selection
			}
			if !decodeJSON(w, r, &body) || !requireSelection(w, body.Selection) {
				return
			}
			bulkDone(w, a.ForceDownload(body.Selection))
		})

	// The bulk switch for disabled links. /api/tasks/enabled can only be handed
	// ids, which the list can only produce for the rows a filter left on screen;
	// this one also takes a whole package, and all:true for "switch every link
	// that is off back on".
	reg.Add(http.MethodPost, "/api/queue/enabled", "switch a selection, a package, or every disabled link on or off",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				app.Selection
				Enabled bool `json:"enabled"`
			}
			if !decodeJSON(w, r, &body) || !requireSelection(w, body.Selection) {
				return
			}
			bulkDone(w, a.SetEnabledIn(body.Selection, body.Enabled))
		})
}
