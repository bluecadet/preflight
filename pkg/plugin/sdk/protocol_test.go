package sdk

import (
	"errors"
	"slices"
	"sort"
	"testing"
)

// TestNegotiateProtocol covers the exact version check as a pure function.
func TestNegotiateProtocol(t *testing.T) {
	tests := []struct {
		name        string
		local, peer protocolHello
		wantErr     bool
	}{
		{"same version", protocolHello{ProtocolVersion: 3}, protocolHello{ProtocolVersion: 3}, false},
		{"peer newer", protocolHello{ProtocolVersion: 3}, protocolHello{ProtocolVersion: 9}, true},
		{"peer older", protocolHello{ProtocolVersion: 3}, protocolHello{ProtocolVersion: 2}, true},
		{"zero-value peer (field absent on the wire)", protocolHello{ProtocolVersion: 3}, protocolHello{}, true},
		{"negative peer", protocolHello{ProtocolVersion: 3}, protocolHello{ProtocolVersion: -1}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			negotiated, err := negotiateProtocol(tc.local, tc.peer)
			if tc.wantErr {
				var pe *ProtocolError
				if !errors.As(err, &pe) {
					t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
				}
				if pe.Local != tc.local.ProtocolVersion || pe.Peer != tc.peer.ProtocolVersion {
					t.Errorf("Local/Peer = %d/%d, want %d/%d", pe.Local, pe.Peer, tc.local.ProtocolVersion, tc.peer.ProtocolVersion)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if negotiated.Version != tc.local.ProtocolVersion {
				t.Errorf("negotiated version = %d, want %d", negotiated.Version, tc.local.ProtocolVersion)
			}
		})
	}
}

// TestNegotiateProtocol_CapabilityIntersection asserts that negotiateProtocol
// folds intersectCapabilities into its result rather than dropping
// capabilities on a successful negotiation.
func TestNegotiateProtocol_CapabilityIntersection(t *testing.T) {
	local := protocolHello{ProtocolVersion: 3, Capabilities: []string{"a", "b"}}
	peer := protocolHello{ProtocolVersion: 3, Capabilities: []string{"b", "c"}}

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
