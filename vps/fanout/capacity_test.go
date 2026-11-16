package main

import "testing"

func TestEstimateCapacityFD(t *testing.T) {
	est := estimateCapacityFrom(capacityInput{
		fdLimit: 1024, memoryAvailable: 1 << 40,
		portFirst: 20000, portLast: 60000, hardwareMax: 1000,
	})
	if est.Recommended != 12 || est.Reason != capacityReasonFD || est.ByFD != 12 {
		t.Fatalf("estimate = %+v; want FD-limited 12", est)
	}
}

func TestEstimateCapacityMemory(t *testing.T) {
	est := estimateCapacityFrom(capacityInput{
		fdLimit: 100000, memoryAvailable: 10 * capacityMemoryPerSlot,
		portFirst: 20000, portLast: 60000, hardwareMax: 1000,
	})
	if est.Recommended != 10 || est.Reason != capacityReasonMemory || est.ByMemory != 10 {
		t.Fatalf("estimate = %+v; want memory-limited 10", est)
	}
}

func TestEstimateCapacityPorts(t *testing.T) {
	est := estimateCapacityFrom(capacityInput{
		fdLimit: 100000, memoryAvailable: 1 << 40,
		portFirst: 20000, portLast: 20010, hardwareMax: 1000,
	})
	if est.Recommended != 11 || est.Reason != capacityReasonPorts || est.ByPorts != 11 {
		t.Fatalf("estimate = %+v; want port-limited 11", est)
	}
}

func TestEstimateCapacityHardware(t *testing.T) {
	est := estimateCapacityFrom(capacityInput{
		fdLimit: 100000, memoryAvailable: 1 << 40,
		portFirst: 20000, portLast: 60000, hardwareMax: 100,
	})
	if est.Recommended != 100 || est.Reason != capacityReasonHardware {
		t.Fatalf("estimate = %+v; want hardware-limited 100", est)
	}
}

func TestEstimateCapacityNeverReturnsZero(t *testing.T) {
	est := estimateCapacityFrom(capacityInput{fdLimit: 100, hardwareMax: 100})
	if est.Recommended != 1 || est.Reason != capacityReasonFD {
		t.Fatalf("estimate = %+v; want minimum 1 with FD reason", est)
	}
}

func TestFreeRandomPortExhausted(t *testing.T) {
	taken := map[int]bool{}
	for port := randPortMin; port <= randPortMax; port++ {
		taken[port] = true
	}
	if _, err := freeRandomPortFor("tcp", taken); err == nil {
		t.Fatal("freeRandomPortFor accepted a fully occupied range")
	}
}
