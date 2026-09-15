// Package rendering draws the shared CRT interface from read-only scene values.
// It owns layout, animation, visual caches, and borrowed frame storage. It does
// not own navigation, requests, file loading, playback, or output devices.
//
// The caller constructs a Scene and calls Renderer.Render serially. It must
// consume the returned pixels before rendering again. Output backends select
// physical presentation after this shared drawing step.
package rendering
