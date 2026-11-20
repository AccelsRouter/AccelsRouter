package authz

const ResourceSupplier = "supplier"

// Supplier permissions have no default role: Root grants them per account, so
// an admin sees supplier records only after an explicit assignment.
var (
	SupplierRead  = Permission{Resource: ResourceSupplier, Action: ActionRead}
	SupplierWrite = Permission{Resource: ResourceSupplier, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceSupplier,
		LabelKey: "Supplier Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View suppliers",
				DescriptionKey: "View supplier records, contracts, attachments, and model price comparisons.",
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit suppliers",
				DescriptionKey: "Create and edit suppliers, take or void contract numbers, and upload attachments.",
			},
		},
	})
}
