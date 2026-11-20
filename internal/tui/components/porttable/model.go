// Package porttable renders the table of network sockets of the host: one row
// per socket with its protocol, endpoints, state and owning process. The panel
// polls the monitor once per period and can signal the process behind the
// selected socket.
package porttable

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

const (
	defaultWidth  = 80
	defaultHeight = 20

	headerRows = 2 // the title row and the column headers
	footerRows = 1
	minRows    = 1

	// defaultInterval is the polling period of the monitor.
	defaultInterval = time.Second
	snapshotTimeout = 3 * time.Second
	actionTimeout   = 5 * time.Second

	// wheelStep is how many rows one notch of the wheel scrolls.
	wheelStep = 3

	columnCount   = 5
	columnGapText = "  "
	columnGap     = len(columnGapText)

	// Natural column widths of the labels plus the minimums a narrow panel
	// shreds them down to before the whole line is clipped.
	protoLabel    = "PROTO"
	endpointLabel = "ENDPOINT"
	stateLabel    = "ESTADO"
	nameLabel     = "PROCESO"
	rssLabel      = "RSS"

	endpointMax = 18
	nameMax     = 20
	protoMin    = 3
	endpointMin = 10
	stateMin    = 6
	nameMin     = 8
	rssMin      = 5

	// columnSeparator joins the fields of the title and the footer.
	columnSeparator = " · "

	filterPrompt      = "/ "
	filterPlaceholder = "filtro: puerto, proceso, pid"
	filterCharLimit   = 64

	// The notices of an action that never reached the monitor.
	noticeNoProcess = "sin proceso identificable"
	noticeProtected = "proceso protegido"

	// maxErrorCells bounds the line the panel shows for a failure it does not recognise.
	maxErrorCells = 160
)

// snapshotMsg carries one reading of the monitor together with the sequence
// number that ordered the request.
type snapshotMsg struct {
	seq      int
	snapshot domain.Snapshot
	err      error
}

// tickMsg re-arms the always-on ticker.
type tickMsg struct{}

// actionMsg carries the outcome of one signal.
type actionMsg struct {
	seq    int
	pid    int
	name   string
	signal domain.Signal
	err    error
}

// Deps are the collaborators of the table. A nil monitor is reported through
// the state line instead of panicking, and an empty keymap falls back to the
// defaults.
type Deps struct {
	Monitor  ports.MonitorService
	Keys     keymap.Registry
	Interval time.Duration // zero => time.Second
}

// sortMode is the active ordering of the table.
type sortMode uint8

const (
	sortPort sortMode = iota
	sortRSS
	sortProcess
	sortPID

	sortModes = 4
)

// String is the label the title shows for the mode.
func (s sortMode) String() string {
	switch s {
	case sortRSS:
		return "rss"
	case sortProcess:
		return "proceso"
	case sortPID:
		return "pid"
	default:
		return "puerto"
	}
}

// next cycles puerto, rss, proceso, pid and back to puerto.
func (s sortMode) next() sortMode {
	return (s + 1) % sortModes
}

// row is one socket of the table with its owner and its preformatted text.
type row struct {
	socket  domain.Socket
	process domain.Process
	text    string
}

// attributed reports whether the row knows the process behind the socket.
func (r row) attributed() bool {
	return r.socket.Attributed() && r.process.PID == r.socket.PID
}

// name is the process label of the row, unknown when nobody owns the socket.
func (r row) name() string {
	if !r.socket.Attributed() {
		return "?"
	}
	if r.process.Name != "" {
		return r.process.Name
	}

	return "pid " + strconv.Itoa(r.socket.PID)
}

// rss is the resident memory of the owner in human units.
func (r row) rss() string {
	if !r.attributed() {
		return "-"
	}

	return humanBytes(r.process.RSS)
}

// layout holds the width of every column of the table.
type layout struct {
	proto    int
	endpoint int
	state    int
	name     int
	rss      int
}

// Model is the ports table; it never performs I/O outside a command.
type Model struct {
	monitor  ports.MonitorService
	keys     keymap.Registry
	interval time.Duration

	// last good snapshot
	sockets   []domain.Socket
	processes map[int]domain.Process
	byPID     map[int][]domain.Socket
	counts    domain.Counts
	loaded    bool

	// table of the active scope, filter and order
	rows   []row
	header string
	cursor int
	offset int

	scope   domain.Scope
	order   sortMode
	filter  string
	needle  string
	input   textinput.Model
	editing bool

	// cached selection, recomputed whenever the table changes
	selected     domain.Socket
	owner        domain.Process
	siblings     []domain.Socket
	hasSelection bool

	loading     bool
	writing     bool
	snapshotSeq int
	actionSeq   int

	err       error
	actionErr error
	notice    string

	width  int
	height int
	bounds image.Rectangle

	rowRects []image.Rectangle
}

// New builds the table for the given dependencies.
func New(deps Deps) Model {
	keys := deps.Keys
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	interval := deps.Interval
	if interval <= 0 {
		interval = defaultInterval
	}

	input := textinput.New()
	input.Prompt = filterPrompt
	input.Placeholder = filterPlaceholder
	input.CharLimit = filterCharLimit
	input.SetVirtualCursor(true)

	// A static cursor keeps the line free of blink timers: the table redraws on
	// every keystroke, so a blinking cursor would only add noise.
	styles := input.Styles()
	styles.Cursor.Blink = false
	input.SetStyles(styles)

	m := Model{
		monitor:   deps.Monitor,
		keys:      keys,
		interval:  interval,
		processes: make(map[int]domain.Process),
		byPID:     make(map[int][]domain.Socket),
		input:     input,
	}
	m.SetBounds(0, 0, defaultWidth, defaultHeight)

	return m
}

// Scope returns the active tab of the table.
func (m Model) Scope() domain.Scope {
	return m.scope
}

// Loading reports whether a snapshot or an action is still in flight.
func (m Model) Loading() bool {
	return m.loading || m.writing || (!m.loaded && m.err == nil)
}

// Err returns the failure of the last action, or the one of the last load.
func (m Model) Err() error {
	if m.actionErr != nil {
		return m.actionErr
	}

	return m.err
}

// CapturesInput reports that the filter input owns the keyboard of the panel.
func (m Model) CapturesInput() bool {
	return m.editing
}

// Counts summarises the last good snapshot, regardless of the active scope.
func (m Model) Counts() domain.Counts {
	return m.counts
}

// Selection returns the selected socket, its owning process, every socket of
// that process ordered by local port and whether the table has a selection. The
// value is cached whenever the table changes, so the call is O(1).
func (m Model) Selection() (domain.Socket, domain.Process, []domain.Socket, bool) {
	if !m.hasSelection {
		return domain.Socket{}, domain.Process{}, nil, false
	}

	return m.selected, m.owner, m.siblings, true
}

// SetBounds places the table in the terminal and recomputes the visible window.
// The coordinates are absolute, so a click maps back to the row under it.
func (m *Model) SetBounds(x, y, width, height int) {
	width, height = max(width, 0), max(height, 0)
	resized := width != m.width

	m.width, m.height = width, height
	m.bounds = image.Rect(x, y, x+width, y+height)
	m.input.SetWidth(max(width-runeLen(filterPrompt)-1, 1))

	if resized {
		m.rebuildRows() // the column widths depend on the width of the panel
	}

	m.sync()
}

// SetScope re-filters the table for the given tab, keeping the selected socket.
func (m *Model) SetScope(scope domain.Scope) {
	if m.scope == scope {
		return
	}

	id, hadSelection := m.selectedID()
	m.scope = scope
	m.rebuildRows()

	if hadSelection {
		m.focusByID(id)
	}

	m.sync()
}

// rebuildRows filters, orders and formats the table; it runs once per change and
// never from View.
func (m *Model) rebuildRows() {
	rows := m.filtered()

	m.sortRows(rows)

	columns := layoutFor(rows, m.width)
	m.header = columns.header()

	for index := range rows {
		rows[index].text = columns.format(rows[index])
	}

	m.rows = rows
}

// filtered returns the sockets of the snapshot that belong to the active scope
// and match the filter, with the data of their owning process.
func (m Model) filtered() []row {
	rows := make([]row, 0, len(m.sockets))
	for _, socket := range m.sockets {
		if !socket.InScope(m.scope) {
			continue
		}

		item := row{socket: socket}
		if socket.PID > 0 {
			if process, ok := m.processes[socket.PID]; ok {
				item.process = process
			}
		}

		if !m.matches(item) {
			continue
		}

		rows = append(rows, item)
	}

	return rows
}

// sortRows orders the table by the active mode with stable tie-breakers.
func (m Model) sortRows(rows []row) {
	slices.SortStableFunc(rows, func(a, b row) int {
		if order := m.compare(a, b); order != 0 {
			return order
		}
		if order := cmp.Compare(a.socket.Local.Port, b.socket.Local.Port); order != 0 {
			return order
		}
		if order := cmp.Compare(a.socket.Protocol, b.socket.Protocol); order != 0 {
			return order
		}

		return cmp.Compare(a.socket.Inode, b.socket.Inode)
	})
}

// compare orders two rows by the active mode alone.
func (m Model) compare(a, b row) int {
	switch m.order {
	case sortRSS:
		return cmp.Compare(b.process.RSS, a.process.RSS)
	case sortProcess:
		return strings.Compare(strings.ToLower(a.name()), strings.ToLower(b.name()))
	case sortPID:
		return cmp.Compare(a.socket.PID, b.socket.PID)
	default:
		return cmp.Compare(a.socket.Local.Port, b.socket.Local.Port)
	}
}

// matches reports whether the row satisfies the active filter.
func (m Model) matches(item row) bool {
	needle := m.needle
	if needle == "" {
		return true
	}

	if containsFold(item.socket.Protocol.String(), needle) ||
		containsFold(item.socket.Local.String(), needle) ||
		containsFold(item.socket.Remote.String(), needle) ||
		containsFold(item.socket.State.String(), needle) ||
		containsFold(strconv.FormatUint(uint64(item.socket.UID), 10), needle) ||
		containsFold(item.process.User, needle) {
		return true
	}

	if !item.socket.Attributed() {
		return false
	}

	return containsFold(strconv.Itoa(item.socket.PID), needle) || containsFold(item.name(), needle)
}

// containsFold is a case-insensitive substring test against a lower-cased needle.
func containsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}

	return strings.Contains(strings.ToLower(haystack), needle)
}

// sync clamps the cursor and the window and refreshes the cached selection.
func (m *Model) sync() {
	rows := len(m.rows)

	m.cursor = min(max(m.cursor, 0), max(rows-1, 0))
	m.offset = min(max(m.offset, 0), max(rows-m.bodyHeight(), 0))
	m.scrollToCursor()
	m.markRows()
	m.syncSelection()
}

// scrollToCursor moves the window so the cursor stays visible.
func (m *Model) scrollToCursor() {
	height := m.bodyHeight()
	if height <= 0 {
		return
	}

	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}

	m.offset = max(m.offset, 0)
}

// markRows stores the rectangle of every visible row, so a click maps back to
// the row under it.
func (m *Model) markRows() {
	m.rowRects = m.rowRects[:0]

	count := min(m.bodyHeight(), max(len(m.rows)-m.offset, 0))
	if count <= 0 || m.width <= 0 {
		return
	}

	top := m.bounds.Min.Y + headerRows + m.filterRows()
	for index := 0; index < count; index++ {
		m.rowRects = append(m.rowRects, image.Rect(m.bounds.Min.X, top+index, m.bounds.Max.X, top+index+1))
	}
}

// rowAt maps terminal coordinates to the index of the row under them.
func (m Model) rowAt(point image.Point) (int, bool) {
	if !point.In(m.bounds) {
		return 0, false
	}

	for index, rect := range m.rowRects {
		if !point.In(rect) {
			continue
		}

		row := m.offset + index
		if row >= len(m.rows) {
			return 0, false
		}

		return row, true
	}

	return 0, false
}

// syncSelection refreshes the cached selection of the cursor.
func (m *Model) syncSelection() {
	m.selected, m.owner, m.siblings, m.hasSelection = domain.Socket{}, domain.Process{}, nil, false
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}

	item := m.rows[m.cursor]

	m.selected = item.socket
	m.owner = item.process
	m.hasSelection = true
	if item.socket.PID > 0 {
		m.siblings = m.byPID[item.socket.PID]
	}
}

// selectedID returns the identity of the selected socket.
func (m Model) selectedID() (string, bool) {
	if !m.hasSelection {
		return "", false
	}

	return m.selected.ID(), true
}

// focusByID moves the cursor to the row of the given socket identity.
func (m *Model) focusByID(id string) {
	for index, item := range m.rows {
		if item.socket.ID() == id {
			m.cursor = index

			return
		}
	}
}

// bodyHeight is the number of rows the table can show.
func (m Model) bodyHeight() int {
	return max(m.height-headerRows-footerRows-m.filterRows(), minRows)
}

// filterRows is the extra line the filter input takes from the body.
func (m Model) filterRows() int {
	if m.editing || m.filter != "" {
		return 1
	}

	return 0
}

// layoutFor derives the column widths from the rows and the width of the panel.
// The process column keeps a floor and the endpoint shrinks before the state,
// so the name stays readable on a narrow panel.
func layoutFor(rows []row, width int) layout {
	columns := layout{
		proto:    runeLen(protoLabel),
		endpoint: runeLen(endpointLabel),
		state:    runeLen(stateLabel),
		name:     runeLen(nameLabel),
		rss:      runeLen(rssLabel),
	}

	for _, item := range rows {
		columns.proto = max(columns.proto, runeLen(item.socket.Protocol.String()))
		columns.endpoint = max(columns.endpoint, runeLen(item.socket.Local.String()))
		columns.state = max(columns.state, runeLen(item.socket.State.String()))
		columns.name = max(columns.name, runeLen(item.name()))
		columns.rss = max(columns.rss, runeLen(item.rss()))
	}

	columns.endpoint = min(columns.endpoint, endpointMax)
	columns.name = min(columns.name, nameMax)

	need := columns.total() - width
	if need <= 0 {
		return columns
	}

	shrunk := columns
	need = take(&shrunk.name, nameMin, need)
	need = take(&shrunk.endpoint, endpointMin, need)
	need = take(&shrunk.state, stateMin, need)
	need = take(&shrunk.rss, rssMin, need)
	take(&shrunk.proto, protoMin, need)

	return shrunk
}

// total is the width the layout occupies, gaps included.
func (l layout) total() int {
	return l.proto + l.endpoint + l.state + l.name + l.rss + columnGap*(columnCount-1)
}

// take removes up to need cells from *width, never below low, and returns what
// is still missing.
func take(width *int, low, need int) int {
	if need <= 0 {
		return 0
	}

	cut := min(*width-low, need)
	if cut <= 0 {
		return need
	}

	*width -= cut

	return need - cut
}

// header is the column header with the same widths as the rows.
func (l layout) header() string {
	var b strings.Builder
	b.Grow(l.total())

	writeCell(&b, protoLabel, l.proto)
	writeCell(&b, endpointLabel, l.endpoint)
	writeCell(&b, stateLabel, l.state)
	writeCell(&b, nameLabel, l.name)
	writeTail(&b, rssLabel, l.rss)

	return b.String()
}

// format renders one row with the widths of the layout.
func (l layout) format(item row) string {
	var b strings.Builder
	b.Grow(l.total())

	writeCell(&b, item.socket.Protocol.String(), l.proto)
	writeEndpoint(&b, item.socket.Local.String(), l.endpoint)
	writeCell(&b, item.socket.State.String(), l.state)
	writeCell(&b, item.name(), l.name)
	writeTail(&b, item.rss(), l.rss)

	return b.String()
}

// writeCell appends one truncated and padded cell and the gap after it.
func writeCell(b *strings.Builder, text string, width int) {
	b.WriteString(padRight(truncate(text, width), width))
	b.WriteString(columnGapText)
}

// writeEndpoint keeps the tail of the endpoint, so the port survives a narrow panel.
func writeEndpoint(b *strings.Builder, text string, width int) {
	b.WriteString(padRight(truncateLeft(text, width), width))
	b.WriteString(columnGapText)
}

// writeTail appends the last cell without padding: View pads the whole line.
func writeTail(b *strings.Builder, text string, width int) {
	b.WriteString(truncate(text, width))
}

// indexSnapshot indexes the processes by PID and groups the sockets of each one
// ordered by local port, so listing the sockets of a process is a lookup.
func indexSnapshot(snapshot domain.Snapshot) (map[int]domain.Process, map[int][]domain.Socket) {
	processes := make(map[int]domain.Process, len(snapshot.Processes))
	for _, process := range snapshot.Processes {
		processes[process.PID] = process
	}

	byPID := make(map[int][]domain.Socket, len(processes))
	for _, socket := range snapshot.Sockets {
		if socket.PID <= 0 {
			continue
		}

		byPID[socket.PID] = append(byPID[socket.PID], socket)
	}

	for _, sockets := range byPID {
		slices.SortFunc(sockets, compareSockets)
	}

	return processes, byPID
}

// compareSockets orders the sockets of one process by local port.
func compareSockets(a, b domain.Socket) int {
	if order := cmp.Compare(a.Local.Port, b.Local.Port); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Protocol, b.Protocol); order != 0 {
		return order
	}

	return cmp.Compare(a.Inode, b.Inode)
}

// humanBytes renders a byte count with one decimal from KiB up.
func humanBytes(size uint64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)

	switch {
	case size >= gib:
		return fmt.Sprintf("%.1f GiB", float64(size)/float64(gib))
	case size >= mib:
		return fmt.Sprintf("%.1f MiB", float64(size)/float64(mib))
	case size >= kib:
		return fmt.Sprintf("%.1f KiB", float64(size)/float64(kib))
	default:
		return strconv.FormatUint(size, 10) + " B"
	}
}

// describeError turns a monitor failure into the message the panel shows.
func describeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrUnavailable):
		return "monitor no disponible"
	case errors.Is(err, domain.ErrUnsupported):
		return "sistema no soportado"
	case errors.Is(err, domain.ErrPermissionDenied):
		return "permiso denegado sobre los procesos del sistema"
	case errors.Is(err, domain.ErrNotFound):
		return "no encontrado"
	case errors.Is(err, domain.ErrInvalidArgument):
		return "argumento inválido"
	case errors.Is(err, domain.ErrProcessProtected):
		return "proceso protegido"
	case errors.Is(err, domain.ErrTooLarge):
		return "demasiados elementos"
	case errors.Is(err, context.DeadlineExceeded):
		return "tiempo agotado"
	case errors.Is(err, context.Canceled):
		return "cancelado"
	default:
		return summarize(err)
	}
}

// summarize reduces an unknown failure to one line without control characters.
func summarize(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")

	return truncate(strings.TrimSpace(strings.Map(dropControl, line)), maxErrorCells)
}

// dropControl keeps the printable runes, plus the tab the renderer already converts.
func dropControl(r rune) rune {
	if r == '\t' {
		return r
	}
	if r < ' ' || r == 0x7f {
		return -1
	}

	return r
}

// truncate shortens text to width cells, marking the cut with an ellipsis.
func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	return string(runes[:width-1]) + "…"
}

// truncateLeft shortens text to width cells, keeping its tail: the port of an
// endpoint says more than its address.
func truncateLeft(text string, width int) string {
	if width <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	return "…" + string(runes[len(runes)-width+1:])
}

// padRight fills text up to width cells.
func padRight(text string, width int) string {
	gap := width - runeLen(text)
	if gap <= 0 {
		return text
	}

	return text + strings.Repeat(" ", gap)
}

// runeLen counts the cells of text; the table renders single-wide runes only.
func runeLen(text string) int {
	return len([]rune(text))
}
