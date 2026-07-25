// Package credstoretest provides a credential store fake for tests.
package credstoretest

import "github.com/open-cli-collective/cli-common/credstore"

// Store is an in-memory credential store with injectable failures.
type Store struct {
	Values       map[string]string
	BackendValue credstore.Backend
	Source       credstore.Source
	GetErr       error
	SetErr       error
	SetErrs      []error
	DeleteErr    error
	ExistsErr    error
	CloseErr     error
	OnSet        func()
	SetCalls     int
	Closed       bool
	Overwrite    bool
}

// Backend returns the configured backend metadata.
func (store *Store) Backend() (credstore.Backend, credstore.Source) {
	return store.BackendValue, store.Source
}

// Close records closure without disabling later inspection or use.
func (store *Store) Close() error {
	store.Closed = true
	return store.CloseErr
}

// Get returns a stored value or credstore.ErrNotFound.
func (store *Store) Get(profile, key string) (string, error) {
	if store.GetErr != nil {
		return "", store.GetErr
	}
	value, ok := store.Values[profile+"/"+key]
	if !ok {
		return "", credstore.ErrNotFound
	}
	return value, nil
}

// Set records the call before applying queued or persistent failures.
func (store *Store) Set(profile, key, value string, opts ...credstore.SetOpt) error {
	store.SetCalls++
	store.Overwrite = len(opts) > 0
	if store.OnSet != nil {
		store.OnSet()
	}
	err := store.SetErr
	if len(store.SetErrs) > 0 {
		err = store.SetErrs[0]
		store.SetErrs = store.SetErrs[1:]
	}
	if err != nil {
		return err
	}
	item := profile + "/" + key
	if _, exists := store.Values[item]; exists && !store.Overwrite {
		return credstore.ErrExists
	}
	if store.Values == nil {
		store.Values = map[string]string{}
	}
	store.Values[item] = value
	return nil
}

// Delete removes a stored value or returns credstore.ErrNotFound.
func (store *Store) Delete(profile, key string) error {
	if store.DeleteErr != nil {
		return store.DeleteErr
	}
	item := profile + "/" + key
	if _, exists := store.Values[item]; !exists {
		return credstore.ErrNotFound
	}
	delete(store.Values, item)
	return nil
}

// Exists reports whether a value is stored.
func (store *Store) Exists(profile, key string) (bool, error) {
	if store.ExistsErr != nil {
		return false, store.ExistsErr
	}
	_, exists := store.Values[profile+"/"+key]
	return exists, nil
}
