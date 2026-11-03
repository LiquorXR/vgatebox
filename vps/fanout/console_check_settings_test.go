package main

import (
	"context"
	"testing"
)

func TestCheckIPTypeContextDefaultsOnAndCanBeDisabled(t *testing.T) {
	if !checkIPTypeFromContext(context.Background()) {
		t.Fatal("candidate checks must check IP type by default")
	}
	ctx := context.WithValue(context.Background(), checkIPTypeContextKey{}, false)
	if checkIPTypeFromContext(ctx) {
		t.Fatal("candidate check type switch was ignored")
	}
}
