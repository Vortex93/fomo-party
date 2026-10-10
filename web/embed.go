package web

import "embed"

// Assets are compiled into the server: one executable serves the entire app.
//
//go:embed index.html styles.css app.js lib/*.js
var Assets embed.FS
