package resolver

import "fmt"

type Route struct {
	Provider string
	Model    string
	API      string
}

type ModelResolver struct {
	routes map[string]Route
}

func New(routes map[string]Route) *ModelResolver {
	return &ModelResolver{routes: routes}
}

func (r *ModelResolver) Resolve(model string) (Route, bool) {
	route, ok := r.routes[model]
	if !ok {
		return Route{}, false
	}
	return route, true
}

func (r *ModelResolver) Keys() []string {
	keys := make([]string, 0, len(r.routes))
	for k := range r.routes {
		keys = append(keys, k)
	}
	return keys
}

func (r *ModelResolver) AddRoute(clientModel string, route Route) error {
	if _, ok := r.routes[clientModel]; ok {
		return fmt.Errorf("route already exists: %s", clientModel)
	}
	r.routes[clientModel] = route
	return nil
}
