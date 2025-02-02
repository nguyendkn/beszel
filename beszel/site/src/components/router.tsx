import { createRouter } from "@nanostores/router"

const routes = {
	home: "/",
	system: `/system/:name`,
	settings: `/settings/:name?`,
	forgot_password: `/forgot-password`,
} as const

// TODO: fix /static links like favicon and check login

// calculate base path based on current path and position of routes
// this is needed to support serving from subpaths like beszel.com/example-base
export const basePath = (() => {
	const baseRoutes = Object.values(routes).map((route) => route.split("/").at(1))
	const pathSegments = window.location.pathname.split("/").filter(Boolean)
	for (let i = 0; i < pathSegments.length; i++) {
		if (baseRoutes.includes(pathSegments[i])) {
			// If a route is found, the base path is everything before this segment
			return "/" + pathSegments.slice(0, i).join("/")
		}
	}
	return "/" + pathSegments.join("/")
})()

export const prependBasePath = (path: string) => `${basePath}${path}`.replaceAll("//", "/")

// prepend base path to routes
for (const route in routes) {
	// @ts-ignore need as const above to get nanostores to parse types properly
	routes[route] = prependBasePath(routes[route])
}

// console.log("routes", routes)

export const $router = createRouter(routes, { links: false })

/** Navigate to url using router
 *  Base path is automatically prepended if serving from subpath
 */
export const navigate = (urlString: string) => {
	$router.open(urlString)
}

function onClick(e: React.MouseEvent<HTMLAnchorElement, MouseEvent>) {
	e.preventDefault()
	$router.open(new URL((e.currentTarget as HTMLAnchorElement).href).pathname)
}

export const Link = (props: React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
	return <a onClick={onClick} {...props}></a>
}
