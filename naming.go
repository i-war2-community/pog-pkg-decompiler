package main

type NameProvider interface {
	GetName() string
}

type StaticNameProvider struct {
	name string
}

func (np *StaticNameProvider) GetName() string {
	return np.name
}

type GlobalNameProvider struct {
	assignment *OpGraph
}

func (np *GlobalNameProvider) GetName() string {
	// Look for the Global call while allowing some other calls along the way
	for node := np.assignment; node.operation.IsFunctionCall() && len(node.children) > 0; node = node.children[0] {
		fd := node.operation.GetFunctionDeclaration()
		if fd.pkg == "Global" {
			switch fd.name {
			case "Int", "Float", "Bool", "String", "Handle", "List", "Set":
				if node.children[0].operation.opcode == OP_UNKNOWN_3B && node.children[0].children[0].operation.opcode == OP_LITERAL_STRING {
					value := node.children[0].children[0].operation.data.(LiteralStringData).String()
					// Strip the quotes
					value = value[1 : len(value)-1]

					if isValidIdentifier(value) {
						return value
					}
				}
			}
		}
	}
	return ""
}

func ResolveToName(providers []NameProvider) string {
	resolvedNames := map[string]bool{}

	// Build a map of the names
	for _, provider := range providers {
		name := provider.GetName()
		if len(name) > 0 {
			resolvedNames[name] = true
		}
	}

	nameCount := len(resolvedNames)

	if nameCount == 1 {
		// Return the first one
		for name := range resolvedNames {
			return name
		}
	} else if nameCount > 1 {
		// TODO: Decide on some heuristic here...
	}

	return ""
}
