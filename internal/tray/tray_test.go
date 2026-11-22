package tray

import "testing"

func TestMenuAndTooltip(t *testing.T) {
	m := Build("Halyard", 3)
	keys := map[string]bool{}
	for _, it := range m.Items {
		keys[it.Key] = true
	}
	for _, k := range []string{"status", "open", "compose", "check", "quit"} {
		if !keys[k] {
			t.Errorf("the menu has no %s item", k)
		}
	}
	if m.Items[0].Label != "3 unread messages" || !m.Items[0].Disabled {
		t.Fatalf("status line: %+v", m.Items[0])
	}
	if Build("Halyard", 1).Items[0].Label != "1 unread message" || Build("Halyard", 0).Items[0].Label != "No unread mail" {
		t.Fatal("singular and none")
	}
	if Tooltip("Halyard", 12) != "Halyard: 12 unread" || Tooltip("Halyard", 0) != "Halyard" {
		t.Fatal("tooltip")
	}
	if len(Icon()) < 100 {
		t.Fatal("no icon is embedded")
	}
}
