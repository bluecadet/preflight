package sdk

import (
	"errors"
	"slices"
	"sort"
	"testing"
)

// TestNegotiateProtocol covers negotiateProtocol as a pure function, apart
// from the full initialize handshake exercised elsewhere. Cases include the
// well-behaved ranges as well as malformed peer input (min > max, negative
// values, all-zero) that a buggy or hostile peer could send: negotiateProtocol
// must reject these safely via ordinary integer comparison, never panic or
// silently accept an invalid range.
func TestNegotiateProtocol(t *testing.T) {
	tests := []struct {
		name        string
		local, peer protocolRange
		wantVersion int
		wantErr     bool
	}{
		{
			name:        "identical ranges negotiate that version",
			local:       protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:        protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			wantVersion: 2,
		},
		{
			name:        "peer range strictly wider negotiates the local max",
			local:       protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:        protocolRange{MinProtocolVersion: 1, ProtocolVersion: 5},
			wantVersion: 2,
		},
		{
			name:        "local range strictly wider negotiates the peer max",
			local:       protocolRange{MinProtocolVersion: 1, ProtocolVersion: 5},
			peer:        protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			wantVersion: 2,
		},
		{
			name:        "partial overlap negotiates the highest shared version",
			local:       protocolRange{MinProtocolVersion: 1, ProtocolVersion: 3},
			peer:        protocolRange{MinProtocolVersion: 3, ProtocolVersion: 5},
			wantVersion: 3,
		},
		{
			name:    "peer strictly below local range fails",
			local:   protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:    protocolRange{MinProtocolVersion: 1, ProtocolVersion: 1},
			wantErr: true,
		},
		{
			name:    "peer strictly above local range fails",
			local:   protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:    protocolRange{MinProtocolVersion: 9, ProtocolVersion: 9},
			wantErr: true,
		},
		{
			name:    "zero-value peer (field absent on the wire) fails",
			local:   protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:    protocolRange{},
			wantErr: true,
		},
		{
			name:    "malformed peer with min greater than max still fails safely",
			local:   protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:    protocolRange{MinProtocolVersion: 5, ProtocolVersion: 2},
			wantErr: true,
		},
		{
			name:    "malformed peer with negative bounds fails safely",
			local:   protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2},
			peer:    protocolRange{MinProtocolVersion: -5, ProtocolVersion: -1},
			wantErr: true,
		},
		{
			// local.Min (5) > local.Max (2) is self-contradictory input that
			// negotiateProtocol never validates explicitly; the plain
			// max/min arithmetic still resolves it safely to "no overlap"
			// rather than panicking or picking a version outside the
			// (nonsensical) local range.
			name:    "malformed local with min greater than max fails safely, not panics",
			local:   protocolRange{MinProtocolVersion: 5, ProtocolVersion: 2},
			peer:    protocolRange{MinProtocolVersion: 1, ProtocolVersion: 10},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			negotiated, err := negotiateProtocol(tc.local, tc.peer)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("negotiateProtocol(%+v, %+v) = %+v, nil; want error", tc.local, tc.peer, negotiated)
				}
				var pe *ProtocolError
				if !errors.As(err, &pe) {
					t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
				}
				if pe.LocalMin != tc.local.MinProtocolVersion || pe.LocalMax != tc.local.ProtocolVersion {
					t.Errorf("LocalMin/LocalMax = %d/%d, want %d/%d", pe.LocalMin, pe.LocalMax, tc.local.MinProtocolVersion, tc.local.ProtocolVersion)
				}
				if pe.PeerMin != tc.peer.MinProtocolVersion || pe.PeerMax != tc.peer.ProtocolVersion {
					t.Errorf("PeerMin/PeerMax = %d/%d, want %d/%d", pe.PeerMin, pe.PeerMax, tc.peer.MinProtocolVersion, tc.peer.ProtocolVersion)
				}
				return
			}
			if err != nil {
				t.Fatalf("negotiateProtocol(%+v, %+v) returned unexpected error: %v", tc.local, tc.peer, err)
			}
			if negotiated.Version != tc.wantVersion {
				t.Errorf("negotiated version = %d, want %d", negotiated.Version, tc.wantVersion)
			}
		})
	}
}

// TestNegotiateProtocol_CapabilityIntersection asserts that negotiateProtocol
// folds intersectCapabilities into its result rather than dropping
// capabilities on a successful negotiation.
func TestNegotiateProtocol_CapabilityIntersection(t *testing.T) {
	local := protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2, Capabilities: []string{"a", "b"}}
	peer := protocolRange{MinProtocolVersion: 2, ProtocolVersion: 2, Capabilities: []string{"b", "c"}}

	negotiated, err := negotiateProtocol(local, peer)
	if err != nil {
		t.Fatalf("negotiateProtocol: %v", err)
	}
	if !negotiated.HasCapability("b") {
		t.Error("expected HasCapability(b) = true")
	}
	if negotiated.HasCapability("a") || negotiated.HasCapability("c") {
		t.Errorf("negotiated capabilities = %v, want exactly [b]", negotiated.Capabilities())
	}
}

// TestIntersectCapabilities covers intersectCapabilities as a pure function,
// including the edge cases negotiateProtocol's own tests do not isolate:
// empty inputs on either side and duplicate names within one side.
func TestIntersectCapabilities(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{
			name: "disjoint sets intersect to empty",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: nil,
		},
		{
			name: "partial overlap",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c", "d"},
			want: []string{"b", "c"},
		},
		{
			name: "nil a yields empty",
			a:    nil,
			b:    []string{"a"},
			want: nil,
		},
		{
			name: "nil b yields empty",
			a:    []string{"a"},
			b:    nil,
			want: nil,
		},
		{
			name: "both nil yields empty",
			a:    nil,
			b:    nil,
			want: nil,
		},
		{
			name: "duplicate names in a collapse to one entry",
			a:    []string{"a", "a", "b"},
			b:    []string{"a"},
			want: []string{"a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := intersectCapabilities(tc.a, tc.b)
			gotSlice := make([]string, 0, len(got))
			for c := range got {
				gotSlice = append(gotSlice, c)
			}
			sort.Strings(gotSlice)
			wantSlice := append([]string{}, tc.want...)
			sort.Strings(wantSlice)
			if !slices.Equal(gotSlice, wantSlice) {
				t.Errorf("intersectCapabilities(%v, %v) = %v, want %v", tc.a, tc.b, gotSlice, wantSlice)
			}
		})
	}
}

// TestNegotiatedFromContext_AbsentReportsNotOK asserts that a context not
// produced by a plugin's Check/Apply reports ok=false rather than a
// zero-valued Negotiated that could be mistaken for a real (if empty)
// negotiation result.
func TestNegotiatedFromContext_AbsentReportsNotOK(t *testing.T) {
	if _, ok := NegotiatedFromContext(t.Context()); ok {
		t.Error("expected ok=false for a context with no Negotiated attached")
	}
}
