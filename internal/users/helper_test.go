package users

import "mdbox/internal/store"

func storeInput(title string) store.CreateInput {
	return store.CreateInput{Title: title, Content: "body"}
}
