package authz

// Business development summary (业务信息汇总): supplier / customer leads and
// the records of supplier models allocated to customers. Not granted to the
// admin role by default — root enables it per admin in the user permission
// editor.
const (
	ResourceBd = "bd"

	ActionManage = "manage"
)

var (
	BdRead   = Permission{Resource: ResourceBd, Action: ActionRead}
	BdWrite  = Permission{Resource: ResourceBd, Action: ActionWrite}
	BdManage = Permission{Resource: ResourceBd, Action: ActionManage}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceBd,
		LabelKey: "Business Summary",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View business summary",
				DescriptionKey: "View supplier and customer leads and model allocation records, and export them as CSV.",
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit leads and allocations",
				DescriptionKey: "Add and edit leads, allocate supplier models to customers, and delete leads and records you created.",
			},
			{
				Action:         ActionManage,
				LabelKey:       "Manage business summary",
				DescriptionKey: "Delete any lead or allocation record.",
			},
		},
	})
}
