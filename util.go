package electrum

func GetAddressFromVout(vout *Vout) string {
	if vout.ScriptPubKey.Address != "" {
		return vout.ScriptPubKey.Address
	}

	if vout.ScriptPubKey.Addresses != nil &&
		len(vout.ScriptPubKey.Addresses) > 0 {
		return vout.ScriptPubKey.Addresses[0]
	}

	return ""
}

// find address in vin and vout and call fn
func FindAddressFunc[E any](
	address string,
	inouts []E,
	fnAddr func(elem E, index int) bool,
) {
	for index, inout := range inouts {
		var vout *Vout

		switch v := any(inout).(type) {
		case VinWithPrevout:
			vout = v.Prevout
		case Vout:
			vout = &v
		default:
			continue
		}

		if vout != nil {
			if GetAddressFromVout(vout) == address {
				if fnAddr(inout, index) {
					continue
				}

				break
			}
		}
	}
}
