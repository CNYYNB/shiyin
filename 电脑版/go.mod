module shiyin

go 1.26.0

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.44.0
)

require github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect

replace github.com/jchv/go-webview2 => ./third_party/go-webview2
