package scout

import "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"

// FragmentType and DependencyType are names for the AH taxonomy types used
// throughout the scout package (call sites migrate incrementally to ah.*).
type FragmentType = ah.FragmentClass
type DependencyType = ah.RelationName
