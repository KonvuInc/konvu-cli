package findings

import (
	"errors"
	"testing"
)

type fakeClient struct {
	responses []map[string]any
	calls     []getCall
	err       error
}

type getCall struct {
	path   string
	params map[string]any
}

func (f *fakeClient) Get(path string, params map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, getCall{path: path, params: params})
	if f.err != nil {
		return nil, f.err
	}
	if len(f.calls) > len(f.responses) {
		return map[string]any{"total": float64(0), "items": []any{}}, nil
	}
	return f.responses[len(f.calls)-1], nil
}

func TestCountByPagination_ReadsTotalFromMetadata(t *testing.T) {
	client := &fakeClient{
		responses: []map[string]any{{"total": float64(42), "items": []any{}}},
	}
	got, err := CountByPagination(client, "/sca_findings", map[string]any{"severity": "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
	if len(client.calls) != 1 || client.calls[0].path != "/sca_findings" {
		t.Fatalf("wrong call: %+v", client.calls)
	}
	if client.calls[0].params["per_page"] != 1 {
		t.Fatalf("per_page not forced to 1: %v", client.calls[0].params["per_page"])
	}
	if client.calls[0].params["severity"] != "high" {
		t.Fatalf("caller params not preserved: %v", client.calls[0].params)
	}
}

func TestCountByPagination_PropagatesError(t *testing.T) {
	client := &fakeClient{err: errors.New("boom")}
	if _, err := CountByPagination(client, "/sca_findings", nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCountByPagination_ZeroWhenTotalMissing(t *testing.T) {
	client := &fakeClient{responses: []map[string]any{{"items": []any{}}}}
	got, err := CountByPagination(client, "/x", nil)
	if err != nil || got != 0 {
		t.Fatalf("got (%d, %v)", got, err)
	}
}
