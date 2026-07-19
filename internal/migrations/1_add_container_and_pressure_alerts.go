package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}

		nameField, ok := collection.Fields.GetByName("name").(*core.SelectField)
		if !ok {
			return nil
		}

		toAdd := []string{
			"CpuPressureAvg10", "CpuPressureAvg60", "CpuPressureAvg300",
			"MemPressureSomeAvg10", "MemPressureSomeAvg60", "MemPressureSomeAvg300",
			"MemPressureFullAvg10", "MemPressureFullAvg60", "MemPressureFullAvg300",
			"IOPressureSomeAvg10", "IOPressureSomeAvg60", "IOPressureSomeAvg300",
			"IOPressureFullAvg10", "IOPressureFullAvg60", "IOPressureFullAvg300",
			"CtxSwitches", "Interrupts",
			"ContainerCPU", "ContainerMem", "ContainerNet",
			"ContainerRestarts", "ContainerOOM",
		}

		existing := make(map[string]struct{}, len(nameField.Values))
		for _, v := range nameField.Values {
			existing[v] = struct{}{}
		}

		for _, v := range toAdd {
			if _, found := existing[v]; !found {
				nameField.Values = append(nameField.Values, v)
			}
		}

		return app.Save(collection)
	}, nil)
}
