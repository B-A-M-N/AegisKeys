package broker

import (
	"os"
	"sync"
	"testing"
)

func BenchmarkConcurrentResolveRotateImmediateRevocation(b *testing.B) {
	test := &testing.T{}
	service, peer := serviceForUnit(test, true, "/opt/app", CapabilityResolve, CapabilityRotate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := service.Resolve("app/service", peer); err != nil {
			b.Fatal(err)
		}
		if err := service.Rotate(Request{Binding: "app/service", Value: "rotated"}, peer); err != nil {
			b.Fatal(err)
		}
	}
}

func TestServiceConcurrentResolveRevocationWithoutStaleCache(t *testing.T) {
	service, peer := serviceForUnit(t, true, "/opt/app", CapabilityResolve)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				if _, err := service.Resolve("app/service", peer); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	service.meta.Grants[0].Enabled = false
	if _, err := service.Resolve("app/service", peer); err != ErrAccessDenied {
		t.Fatalf("revoked stale cache accepted: %v", err)
	}
	_ = os.Getpid()
}
