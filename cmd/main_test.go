package main

import (
	"testing"

	"go.uber.org/fx"
)

// TestAppDependencyGraphIsComplete proves every constructor in the graph has
// a provider for each of its parameters. fx.ValidateApp resolves the graph
// without invoking constructors or lifecycle hooks, so no database, Redis or
// Kafka is needed.
func TestAppDependencyGraphIsComplete(t *testing.T) {
	if err := fx.ValidateApp(appOptions()...); err != nil {
		t.Fatalf("the fx dependency graph does not resolve: %v", err)
	}
}
