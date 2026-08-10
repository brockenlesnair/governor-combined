package search

import (
	"context"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

type GraphSearch struct {
	cg *callgraph.Graph
}

func NewGraphSearch(cg *callgraph.Graph) *GraphSearch {
	return &GraphSearch{cg: cg}
}

func (gs *GraphSearch) CallersOf(ctx context.Context, targetID string, depth int) []*callgraph.Edge {
	if depth <= 0 {
		depth = 10
	}

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	return gs.cg.TransitiveCallers(targetID, depth)
}

func (gs *GraphSearch) CalleesOf(ctx context.Context, targetID string, depth int) []*callgraph.Edge {
	if depth <= 0 {
		depth = 10
	}

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	return gs.cg.TransitiveCallees(targetID, depth)
}

func (gs *GraphSearch) FindBySignature(ctx context.Context, filter *TypeFilter) []*callgraph.Node {
	return findBySignature(ctx, gs.cg.Nodes, filter)
}

func (gs *GraphSearch) CallerCount(entityID string) int {
	return len(gs.cg.Callers(entityID))
}

func (gs *GraphSearch) CalleeCount(entityID string) int {
	return len(gs.cg.Callees(entityID))
}
