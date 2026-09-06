package gb28181

import "fmt"

func shutdownComponentError(component string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", component, err)
}
