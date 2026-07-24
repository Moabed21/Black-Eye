package process

import (
	"fmt"
	"sort"
)

// TreeNode represents a node in the process hierarchy tree.
type TreeNode struct {
	Proc     ProcessSnapshot
	Depth    int
	Children []*TreeNode
}

// BuildProcessTree organizes a flat slice of ProcessSnapshots into a tree hierarchy.
func BuildProcessTree(procs []ProcessSnapshot) []*TreeNode {
	byPID := make(map[int]*TreeNode)
	for _, p := range procs {
		byPID[p.PID] = &TreeNode{
			Proc:     p,
			Children: []*TreeNode{},
		}
	}

	var roots []*TreeNode

	for _, p := range procs {
		node := byPID[p.PID]
		parent, exists := byPID[p.PPID]
		if exists && p.PPID != p.PID {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}

	// Sort children recursively by PID
	var sortNodes func(nodes []*TreeNode, depth int)
	sortNodes = func(nodes []*TreeNode, depth int) {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Proc.PID < nodes[j].Proc.PID
		})
		for _, n := range nodes {
			n.Depth = depth
			if len(n.Children) > 0 {
				sortNodes(n.Children, depth+1)
			}
		}
	}

	sortNodes(roots, 0)
	return roots
}

// FlattenTree flattens a tree hierarchy into a ordered list of ProcessSnapshots
// with indent branch connectors (├── └──) added to DisplayName for UI rendering.
func FlattenTree(roots []*TreeNode) []ProcessSnapshot {
	var result []ProcessSnapshot

	var walk func(nodes []*TreeNode, prefix string)
	walk = func(nodes []*TreeNode, prefix string) {
		count := len(nodes)
		for i, n := range nodes {
			isLast := (i == count-1)
			branch := "├── "
			childPrefix := prefix + "│   "
			if isLast {
				branch = "└── "
				childPrefix = prefix + "    "
			}

			proc := n.Proc
			if n.Depth > 0 {
				proc.DisplayName = fmt.Sprintf("%s%s%s", prefix, branch, proc.DisplayName)
			}

			result = append(result, proc)

			if len(n.Children) > 0 {
				walk(n.Children, childPrefix)
			}
		}
	}

	walk(roots, "")
	return result
}
