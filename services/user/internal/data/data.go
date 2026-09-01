package data

type StoreSet struct {
	UserRepository     any
	RoleRepository     any
	RefreshTokenStore  any
	PasswordHashDriver any
	TokenIssuer         any
}

func NewStoreSet() StoreSet {
	return StoreSet{}
}
