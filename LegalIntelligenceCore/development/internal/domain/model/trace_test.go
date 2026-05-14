package model

import (
	"encoding/json"
	"testing"
)

func TestTraceContext_IsZero(t *testing.T) {
	if !(TraceContext{}).IsZero() {
		t.Fatal("empty TraceContext should be zero")
	}
	if (TraceContext{TraceParent: "x"}).IsZero() {
		t.Fatal("TraceContext with TraceParent must not be zero")
	}
	if (TraceContext{TraceState: "y"}).IsZero() {
		t.Fatal("TraceContext with TraceState must not be zero")
	}
}

func TestTraceContext_JSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		tc   TraceContext
		want string
	}{
		{
			name: "full",
			tc:   TraceContext{TraceParent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", TraceState: "vendor=v1"},
			want: `{"traceparent":"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01","tracestate":"vendor=v1"}`,
		},
		{
			name: "no_state_omitted",
			tc:   TraceContext{TraceParent: "00-aa-bb-01"},
			want: `{"traceparent":"00-aa-bb-01"}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(c.tc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != c.want {
				t.Fatalf("got %s, want %s", b, c.want)
			}
			var got TraceContext
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != c.tc {
				t.Fatalf("round-trip mismatch: got %+v, want %+v", got, c.tc)
			}
		})
	}
}
