package pricing

import "testing"

func TestSyntheticModelIsNonBillableAndSilent(t *testing.T) {
	tbl, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalculator(tbl)
	warns := 0
	c.OnWarn = func(string) { warns++ }

	if got := c.CostFor("<synthetic>", 10, 10, 10, 10); got != nil {
		t.Errorf("CostFor(<synthetic>) = %v, want nil", *got)
	}
	if warns != 0 {
		t.Errorf("OnWarn called %d times for <synthetic>, want 0", warns)
	}
}
