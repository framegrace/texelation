package texel

import "testing"

func TestControlBusRegisterTrigger(t *testing.T) {
	bus := NewControlBus()
	called := false
	err := bus.Register("demo.toggle", "demo", func(payload interface{}) error {
		if payload != nil {
			t.Fatalf("unexpected payload: %v", payload)
		}
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("register control: %v", err)
	}

	caps := bus.Capabilities()
	if len(caps) != 1 || caps[0].ID != "demo.toggle" {
		t.Fatalf("capabilities not reported: %+v", caps)
	}

	if err := bus.Trigger("demo.toggle", nil); err != nil {
		t.Fatalf("trigger control: %v", err)
	}
	if !called {
		t.Fatal("control handler was not invoked")
	}
}

func TestControlBusDuplicateRegistration(t *testing.T) {
	bus := NewControlBus()
	h := func(interface{}) error { return nil }
	if err := bus.Register("demo", "demo", h); err != nil {
		t.Fatalf("unexpected error registering control: %v", err)
	}
	if err := bus.Register("demo", "other", h); err == nil {
		t.Fatal("expected duplicate registration to fail")
	}
	if err := bus.Trigger("unknown", nil); err == nil {
		t.Fatal("expected trigger on unknown id to fail")
	}
}
