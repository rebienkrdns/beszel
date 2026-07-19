import { defineConfig } from "vite"
import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react-swc"
import { lingui } from "@lingui/vite-plugin"

export default defineConfig(({ command }) => ({
	base: command === "serve" ? "/" : "./",
	plugins: [
		react({
			plugins: [["@lingui/swc-plugin", {}]],
		}),
		lingui(),
		tailwindcss(),
		{
			name: "inject-beszel-dev-config",
			transformIndexHtml(html: string) {
				if (command !== "serve") return html
				return html.replace(
					`globalThis.BESZEL = "{info}"`,
					`globalThis.BESZEL = {"BASE_PATH":"/","HUB_VERSION":"dev","HUB_URL":"","OAUTH_DISABLE_POPUP":false}`
				)
			},
		},
	],
	esbuild: {
		legalComments: "external",
	},
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
		},
	},
	server: {
		proxy: {
			"/api": {
				target: process.env.HUB_URL ?? "http://localhost:8090",
				changeOrigin: true,
				ws: true,
			},
			"/_": {
				target: process.env.HUB_URL ?? "http://localhost:8090",
				changeOrigin: true,
			},
		},
	},
}))
