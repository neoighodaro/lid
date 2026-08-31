package ui

import "strings"

const logo = `
  ██╗     ██╗██████╗
  ██║     ██║██╔══██╗
  ██║     ██║██║  ██║
  ██║     ██║██║  ██║
  ███████╗██║██████╔╝
  ╚══════╝╚═╝╚═════╝`

func RenderLogo() string {
	return Accent.Render(strings.Trim(logo, "\n"))
}
