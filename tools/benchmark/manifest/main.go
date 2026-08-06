package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"wowdata/internal/app"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type manifest struct {
	Schema   string        `json:"schema"`
	Commands []commandLeaf `json:"commands"`
}

type commandLeaf struct {
	Path       string     `json:"path"`
	Use        string     `json:"use"`
	Short      string     `json:"short"`
	Positional []string   `json:"positional,omitempty"`
	Flags      []flagInfo `json:"flags,omitempty"`
}

type flagInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Shorthand string `json:"shorthand,omitempty"`
}

func main() {
	root := app.NewRootCommand()
	leaves := make([]commandLeaf, 0, 32)
	collectLeaves(root, &leaves)
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].Path < leaves[j].Path })
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest{Schema: "wowdata.command-manifest.v1", Commands: leaves}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collectLeaves(command *cobra.Command, output *[]commandLeaf) {
	children := command.Commands()
	if len(children) == 0 {
		path := strings.TrimPrefix(command.CommandPath(), "wowdata ")
		leaf := commandLeaf{Path: path, Use: command.Use, Short: command.Short}
		fields := strings.Fields(command.Use)
		if len(fields) > 1 {
			leaf.Positional = fields[1:]
		}
		seen := make(map[string]struct{})
		appendFlags := func(flags *pflag.FlagSet) {
			flags.VisitAll(func(flag *pflag.Flag) {
				if _, ok := seen[flag.Name]; ok {
					return
				}
				seen[flag.Name] = struct{}{}
				leaf.Flags = append(leaf.Flags, flagInfo{Name: flag.Name, Type: flag.Value.Type(), Default: flag.DefValue, Shorthand: flag.Shorthand})
			})
		}
		appendFlags(command.Flags())
		appendFlags(command.InheritedFlags())
		appendFlags(command.Root().PersistentFlags())
		sort.Slice(leaf.Flags, func(i, j int) bool { return leaf.Flags[i].Name < leaf.Flags[j].Name })
		*output = append(*output, leaf)
		return
	}
	for _, child := range children {
		collectLeaves(child, output)
	}
}
