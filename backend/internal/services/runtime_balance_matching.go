package services

// Each move consumes one donor quota, one instance, one physical target slot
// and one receiver admission claim. Reverse edges can reassign an earlier
// flexible match when a later instance needs its only compatible slot.
type runtimeBalanceCandidate struct {
	move           RuntimeBalanceMove
	source, target int
}

type runtimeBalanceFlowEdge struct{ to, reverse, capacity int }
type runtimeBalanceFlow struct {
	edges  [][]runtimeBalanceFlowEdge
	demand []int
}

func (g *runtimeBalanceFlow) node() int {
	index := len(g.edges)
	g.edges = append(g.edges, nil)
	g.demand = append(g.demand, 0)
	return index
}
func (g *runtimeBalanceFlow) edge(from, to, capacity int) int {
	index := len(g.edges[from])
	reverse := len(g.edges[to])
	g.edges[from] = append(g.edges[from], runtimeBalanceFlowEdge{to, reverse, capacity})
	g.edges[to] = append(g.edges[to], runtimeBalanceFlowEdge{from, index, 0})
	return index
}
func (g *runtimeBalanceFlow) bound(from, to, lower, upper int) bool {
	if lower < 0 || upper < lower {
		return false
	}
	g.edge(from, to, upper-lower)
	g.demand[from] -= lower
	g.demand[to] += lower
	return true
}
func (g *runtimeBalanceFlow) maximum(source, sink, limit int) int {
	total := 0
	for total < limit {
		parent, position := make([]int, len(g.edges)), make([]int, len(g.edges))
		for i := range parent {
			parent[i] = -1
		}
		parent[source] = source
		queue := []int{source}
		for head := 0; head < len(queue) && parent[sink] < 0; head++ {
			from := queue[head]
			for index, edge := range g.edges[from] {
				if edge.capacity <= 0 || parent[edge.to] >= 0 {
					continue
				}
				parent[edge.to], position[edge.to] = from, index
				queue = append(queue, edge.to)
			}
		}
		if parent[sink] < 0 {
			break
		}
		amount := limit - total
		for to := sink; to != source; to = parent[to] {
			if capacity := g.edges[parent[to]][position[to]].capacity; capacity < amount {
				amount = capacity
			}
		}
		for to := sink; to != source; to = parent[to] {
			from, index := parent[to], position[to]
			reverse := g.edges[from][index].reverse
			g.edges[from][index].capacity -= amount
			g.edges[to][reverse].capacity += amount
		}
		total += amount
	}
	return total
}

// Water filling determines a level; only ownership of its final remainder is
// flexible. Fixed occupants and smaller Pod capacities keep their exact bounds.
func runtimeBalanceCountBounds(goal, capacity, fixed []int) ([]int, []int) {
	level := -1
	for i, count := range goal {
		if count < capacity[i] && (level < 0 || count < level) {
			level = count
		}
	}
	if level < 0 {
		level = 0
		for _, count := range goal {
			if count > level {
				level = count
			}
		}
	}
	lower, upper := make([]int, len(goal)), make([]int, len(goal))
	for i := range goal {
		lower[i], upper[i] = level, level+1
		if lower[i] > capacity[i] {
			lower[i] = capacity[i]
		}
		if upper[i] > capacity[i] {
			upper[i] = capacity[i]
		}
		if lower[i] < fixed[i] {
			lower[i] = fixed[i]
		}
		if upper[i] < fixed[i] {
			upper[i] = fixed[i]
		}
	}
	return lower, upper
}

// First solve the allowed floor/ceil distribution with exactly the theoretical
// minimum number of direct moves. If that is infeasible, return safe partial
// progress toward the deterministic original goal, without adding extra moves.
func runtimeBalanceMatch(candidates []runtimeBalanceCandidate, before, goal, lower, upper, admission []int) []int {
	minimum := 0
	for i, count := range before {
		if count > goal[i] {
			minimum += count - goal[i]
		}
	}
	if minimum == 0 {
		return nil
	}
	solve := func(lo, hi []int, exact bool) ([]int, bool) {
		graph := &runtimeBalanceFlow{}
		source, sink := graph.node(), graph.node()
		donors, receivers := make([]int, len(before)), make([]int, len(before))
		for i, count := range before {
			donors[i], receivers[i] = -1, -1
			if count > lo[i] {
				donors[i] = graph.node()
				required := count - hi[i]
				if required < 0 {
					required = 0
				}
				if !exact {
					required = 0
				}
				if !graph.bound(source, donors[i], required, count-lo[i]) {
					return nil, false
				}
			}
			if count < hi[i] {
				receivers[i] = graph.node()
				required, available := lo[i]-count, hi[i]-count
				if required < 0 || !exact {
					required = 0
				}
				if available > admission[i] {
					available = admission[i]
				}
				if !graph.bound(receivers[i], sink, required, available) {
					return nil, false
				}
			}
		}
		instances := map[int]int{}
		type slotKey struct{ pod, slot int }
		slots := map[slotKey]int{}
		type connection struct{ candidate, from, edge int }
		connections := []connection{}
		for i, candidate := range candidates {
			if donors[candidate.source] < 0 || receivers[candidate.target] < 0 {
				continue
			}
			instance, ok := instances[candidate.move.InstanceID]
			if !ok {
				instance = graph.node()
				instances[candidate.move.InstanceID] = instance
				graph.edge(donors[candidate.source], instance, 1)
			}
			key := slotKey{candidate.target, candidate.move.TargetSlot}
			slot, ok := slots[key]
			if !ok {
				slot = graph.node()
				slots[key] = slot
				graph.edge(slot, receivers[candidate.target], 1)
			}
			connections = append(connections, connection{i, instance, graph.edge(instance, slot, 1)})
		}
		if exact {
			graph.bound(sink, source, minimum, minimum)
			superSource, superSink := graph.node(), graph.node()
			required := 0
			for node, demand := range graph.demand {
				if demand > 0 {
					graph.edge(superSource, node, demand)
					required += demand
				} else if demand < 0 {
					graph.edge(node, superSink, -demand)
				}
			}
			if graph.maximum(superSource, superSink, required) != required {
				return nil, false
			}
		} else {
			graph.maximum(source, sink, minimum)
		}
		matched := []int{}
		for _, connection := range connections {
			if graph.edges[connection.from][connection.edge].capacity == 0 {
				matched = append(matched, connection.candidate)
			}
		}
		return matched, true
	}
	if matched, ok := solve(lower, upper, true); ok {
		return matched
	}
	matched, _ := solve(goal, goal, false)
	return matched
}
