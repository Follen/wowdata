package main

import (
	"sort"
	"strings"
	"testing"

	"wowdata/internal/resource"

	"github.com/spf13/cobra"
)

func TestAccountingCoverageRegistersEveryLeaf(t *testing.T) {
	root := newRootCommandForRuntime(NewRuntime())
	var leaves []string
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		children := command.Commands()
		if len(children) == 0 {
			leaves = append(leaves, strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), root.Name())))
			return
		}
		for _, child := range children {
			visit(child)
		}
	}
	visit(root)
	sort.Strings(leaves)
	for _, path := range leaves {
		if _, ok := accountingCoverage[path]; !ok {
			t.Errorf("new CLI leaf %q has no accounting coverage entry", path)
		}
	}
	for path := range accountingCoverage {
		index := sort.SearchStrings(leaves, path)
		if index >= len(leaves) || leaves[index] != path {
			t.Errorf("accounting coverage contains stale leaf %q", path)
		}
	}
}

func TestAccountingFindsLeafAfterPersistentTargetFlags(t *testing.T) {
	resource.ResetWorkMetrics()
	root := newRootCommandForRuntime(NewRuntime())
	beginCommandAccounting(root, []string{"--source", "remote", "--region", "cn", "file", "encoding", "--file-data-id", "1"})
	if snapshot := resource.SnapshotWorkMetrics(); !snapshot.Complete || len(snapshot.UncoveredClasses) != 0 {
		t.Fatalf("flagged leaf accounting = %+v", snapshot)
	}
}
