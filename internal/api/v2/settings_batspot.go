package api

import (
	"fmt"

	"github.com/tphakala/birdnet-go/internal/conf"
)

func modelSettingsSectionValue(settings *conf.Settings, section string) (any, error) {
	if section == conf.ModelIDBatSpot {
		return &settings.BatSpot, nil
	}
	return nil, fmt.Errorf("unknown settings section: %s", section)
}
