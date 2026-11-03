package main

import "testing"

// A commit that removed `users/:id` was reported as breaking
// `api.get('/reports${params}')`: the endpoint's parameter matched an unrelated
// single-segment path. A match needs a literal segment in common.
func TestSameShapeNeedsALiteralSegment(t *testing.T) {
	cases := []struct {
		call, endpoint string
		want           bool
	}{
		{"GET /reports", "GET /api/v1/users/:id", false},
		{"GET /stats", "GET /api/v1/users/:id", false},
		{"GET /tasks/:id", "GET /api/v1/users/:id", false},
		{"GET /tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks", "GET /api/v1/tasks", true},
		{"DELETE /api/v1/tasks/:id", "GET /api/v1/tasks/:id", false},
	}
	for _, c := range cases {
		if got := sameShape(c.endpoint, c.call); got != c.want {
			t.Errorf("sameShape(%q, %q) = %v, want %v", c.endpoint, c.call, got, c.want)
		}
	}
}
