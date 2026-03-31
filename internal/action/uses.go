package action

// PlaybookUses returns all distinct uses refs referenced directly by the
// playbook's tasks, preserving first-seen order.
func PlaybookUses(pb *Playbook) []string {
	if pb == nil {
		return nil
	}
	return collectTaskUses(pb.Tasks)
}

// ActionUses returns all distinct uses refs referenced directly by the action's
// tasks, preserving first-seen order.
func ActionUses(a *Action) []string {
	if a == nil {
		return nil
	}
	return collectTaskUses(a.Tasks)
}

func collectTaskUses(tasks []Task) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, task := range tasks {
		if task.Uses == "" || seen[task.Uses] {
			continue
		}
		seen[task.Uses] = true
		refs = append(refs, task.Uses)
	}
	return refs
}
