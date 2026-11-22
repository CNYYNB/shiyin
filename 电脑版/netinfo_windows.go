//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// adapterInfos reads every adapter's type, description and whether it has a default gateway
// (GetAdaptersAddresses), keyed by friendly name (= net.Interface.Name on Windows).
func adapterInfos() map[string]adapterInfo {
	out := map[string]adapterInfo{}
	size := uint32(15000)
	for i := 0; i < 3; i++ {
		buf := make([]byte, size)
		aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, windows.GAA_FLAG_INCLUDE_GATEWAYS|windows.GAA_FLAG_SKIP_MULTICAST|windows.GAA_FLAG_SKIP_ANYCAST, 0, aa, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return out
		}
		for a := aa; a != nil; a = a.Next {
			name := windows.UTF16PtrToString(a.FriendlyName)
			gwip := ""
			if a.FirstGatewayAddress != nil {
				if ip := a.FirstGatewayAddress.Address.IP(); ip != nil {
					gwip = ip.String()
				}
			}
			out[name] = adapterInfo{
				Gateway:   a.FirstGatewayAddress != nil,
				GatewayIP: gwip,
				IfType:    a.IfType,
				Desc:      windows.UTF16PtrToString(a.Description),
				Known:     true,
			}
		}
		break
	}
	return out
}
