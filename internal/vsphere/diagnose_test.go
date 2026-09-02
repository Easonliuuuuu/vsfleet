package vsphere

import (
	"errors"
	"testing"
)

// These exercise diagRunner and Diagnosis directly, with no vcsim and no
// network — the sequencing and naming rules they encode are exact enough to
// deserve fast, isolated coverage instead of only being proven indirectly by
// the much slower integration tests in tests/.

func TestDiagRunnerSkipsEverythingAfterTheFirstFailure(t *testing.T) {
	d := &Diagnosis{}
	r := &diagRunner{d: d}

	r.run("first", func() (string, error) { return "ok", nil })
	r.run("second", func() (string, error) { return "", errors.New("boom") })
	r.run("third", func() (string, error) {
		t.Fatal("third stage ran after a failure; it should have been skipped")
		return "", nil
	})
	r.skip("fourth", "not applicable")

	if len(d.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(d.Checks))
	}
	if got := d.Checks[0].Status; got != CheckPass {
		t.Errorf("first: status = %s, want pass", got)
	}
	if got := d.Checks[1].Status; got != CheckFail {
		t.Errorf("second: status = %s, want fail", got)
	}
	if got := d.Checks[2].Status; got != CheckSkip {
		t.Errorf("third: status = %s, want skip", got)
	}
	if got := d.Checks[2].Detail; got != "" {
		t.Errorf("third: detail = %q, want empty — a skipped stage never ran fn", got)
	}
	// skip() runs unconditionally, independent of an earlier failure, and
	// keeps whatever detail it was given.
	if got := d.Checks[3].Status; got != CheckSkip {
		t.Errorf("fourth: status = %s, want skip", got)
	}
	if got := d.Checks[3].Detail; got != "not applicable" {
		t.Errorf("fourth: detail = %q, want %q", got, "not applicable")
	}
}

func TestDiagRunnerRunClassifiedRenamesOnlyAMatchingFailure(t *testing.T) {
	sentinel := errors.New("proxy said no")

	t.Run("matching error uses the alternate name", func(t *testing.T) {
		d := &Diagnosis{}
		r := &diagRunner{d: d}
		r.runClassified("TCP connection", "Proxy authentication",
			func(err error) bool { return errors.Is(err, sentinel) },
			func() (string, error) { return "", sentinel })

		c := d.Checks[0]
		if c.Name != "Proxy authentication" {
			t.Errorf("name = %q, want %q", c.Name, "Proxy authentication")
		}
		if c.Status != CheckFail {
			t.Errorf("status = %s, want fail", c.Status)
		}
		if !errors.Is(c.Err, sentinel) {
			t.Errorf("err = %v, want it to wrap the sentinel", c.Err)
		}
	})

	t.Run("non-matching error keeps the default name", func(t *testing.T) {
		d := &Diagnosis{}
		r := &diagRunner{d: d}
		r.runClassified("TCP connection", "Proxy authentication",
			func(err error) bool { return errors.Is(err, sentinel) },
			func() (string, error) { return "", errors.New("connection refused") })

		if got := d.Checks[0].Name; got != "TCP connection" {
			t.Errorf("name = %q, want %q", got, "TCP connection")
		}
	})

	t.Run("success keeps the default name regardless of the classifier", func(t *testing.T) {
		d := &Diagnosis{}
		r := &diagRunner{d: d}
		r.runClassified("TCP connection", "Proxy authentication",
			func(err error) bool {
				t.Fatal("classifier called on a successful stage")
				return true
			},
			func() (string, error) { return "10.0.0.1:443", nil })

		c := d.Checks[0]
		if c.Name != "TCP connection" || c.Status != CheckPass {
			t.Errorf("got %+v, want a passing TCP connection check", c)
		}
	})

	t.Run("skipped after an earlier failure keeps the default name", func(t *testing.T) {
		d := &Diagnosis{}
		r := &diagRunner{d: d}
		r.failed = true
		r.runClassified("TCP connection", "Proxy authentication",
			func(err error) bool { return errors.Is(err, sentinel) },
			func() (string, error) {
				t.Fatal("fn ran on an already-failed runner")
				return "", nil
			})

		c := d.Checks[0]
		if c.Name != "TCP connection" || c.Status != CheckSkip {
			t.Errorf("got %+v, want a skipped TCP connection check", c)
		}
	})
}

func TestDiagnosisOKAndErr(t *testing.T) {
	t.Run("all passing or skipped is OK", func(t *testing.T) {
		d := &Diagnosis{Checks: []Check{
			{Name: "a", Status: CheckPass},
			{Name: "b", Status: CheckSkip},
		}}
		if !d.OK() {
			t.Error("OK() = false, want true")
		}
		if err := d.Err(); err != nil {
			t.Errorf("Err() = %v, want nil", err)
		}
	})

	t.Run("any failure is not OK and Err reports the first one", func(t *testing.T) {
		want := errors.New("proxy unreachable")
		d := &Diagnosis{Checks: []Check{
			{Name: "a", Status: CheckPass},
			{Name: "b", Status: CheckFail, Err: want},
			{Name: "c", Status: CheckSkip},
		}}
		if d.OK() {
			t.Error("OK() = true, want false")
		}
		if err := d.Err(); !errors.Is(err, want) {
			t.Errorf("Err() = %v, want it to wrap %v", err, want)
		}
	})

	t.Run("a failed check with no error still reports one", func(t *testing.T) {
		d := &Diagnosis{Checks: []Check{{Name: "a", Status: CheckFail}}}
		if err := d.Err(); err == nil {
			t.Error("Err() = nil, want a synthesized error naming the stage")
		}
	})
}
