package authz

// Business development summary (业务信息汇总): supplier / customer leads and
// purchase / sale records. Not granted to the admin role by default — root
// enables it per admin in the user permission editor.
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
				DescriptionKey: "View supplier and customer leads, purchase and sale records, and export them as CSV.",
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit leads and deals",
				DescriptionKey: "Add and edit leads, record purchases and sales, and delete leads you created.",
			},
			{
				Action:         ActionManage,
				LabelKey:       "Manage business summary",
				DescriptionKey: "Delete any lead or deal, and set the supplier and supply price of sale records.",
			},
		},
	})
}
