package electrum

func GetAddressFromVout(vout Vout) string {
	if vout.ScriptPubKey.Address != "" {
		return vout.ScriptPubKey.Address
	}

	if vout.ScriptPubKey.Addresses != nil &&
		len(vout.ScriptPubKey.Addresses) > 0 {
		return vout.ScriptPubKey.Addresses[0]
	}

	return ""
}
