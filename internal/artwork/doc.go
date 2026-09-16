// Package artwork loads immutable server images and retains decoded artwork
// in bounded memory and account-scoped disk caches. It owns image identity,
// revision guards, cache formats, and file budgets. Callers own request lifetimes,
// library metadata freshness, selection ordering, and rendering.
package artwork
