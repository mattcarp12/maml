package ast

import (
	"fmt"
	"sync"
)

// SymID is a 32-bit interned string handle.
// On the wire it serializes as a varint (1–5 bytes); common names usually cost 1–2.
type SymID int32

const NoSymbol SymID = -1

// SymbolTable deduplicates strings for an entire compilation unit.
type SymbolTable struct {
	mu   sync.RWMutex
	syms []string
	idx  map[string]SymID
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		syms: make([]string, 0, 256),
		idx:  make(map[string]SymID, 256),
	}
}

// Intern returns the SymID for s, allocating one if necessary.
func (st *SymbolTable) Intern(s string) SymID {
	if s == "" {
		return NoSymbol
	}

	// Fast path: read-only lock.
	st.mu.RLock()
	if id, ok := st.idx[s]; ok {
		st.mu.RUnlock()
		return id
	}
	st.mu.RUnlock()

	// Slow path: promote to write lock with double-check.
	st.mu.Lock()
	defer st.mu.Unlock()
	if id, ok := st.idx[s]; ok {
		return id
	}
	id := SymID(len(st.syms))
	st.syms = append(st.syms, s)
	st.idx[s] = id
	return id
}

// Resolve returns the original string for an ID.
func (st *SymbolTable) Resolve(id SymID) string {
	if id == NoSymbol || int(id) >= len(st.syms) {
		return ""
	}
	return st.syms[id]
}

// All returns a snapshot of every interned string, in ID order.
// Use this to write the symbol table during serialization.
func (st *SymbolTable) All() []string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]string, len(st.syms))
	copy(out, st.syms)
	return out
}

// Len returns how many symbols are interned.
func (st *SymbolTable) Len() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.syms)
}

// -----------------------------------------------------------------------------
// Debug / String() support
// -----------------------------------------------------------------------------

var debugST *SymbolTable

// SetDebugSymbolTable sets the table used by Node.String() methods.
// Call this before printing an AST in tests or in the REPL.
func SetDebugSymbolTable(st *SymbolTable) { debugST = st }

// ResolveDebug returns the string for an ID using the debug table.
func ResolveDebug(id SymID) string {
	if debugST != nil {
		return debugST.Resolve(id)
	}
	return fmt.Sprintf("<sym:%d>", id)
}
