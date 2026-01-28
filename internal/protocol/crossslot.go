package protocol

type keyExtractor func(args [][]byte) [][]byte

var multiKeyCommands = map[string]keyExtractor{
	"MGET":   extractAllKeys,
	"MSET":   extractMSetKeys,
	"DEL":    extractAllKeys,
	"EXISTS": extractAllKeys,
	"RENAME": extractAllKeys,
}

func extractAllKeys(args [][]byte) [][]byte {
	return args
}

func extractMSetKeys(args [][]byte) [][]byte {
	if len(args) < 2 {
		return nil
	}
	keys := make([][]byte, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		keys = append(keys, args[i])
	}
	return keys
}

func isMultiKeyCommand(cmd string) bool {
	_, ok := multiKeyCommands[cmd]
	return ok
}

func isMultiKeyCommandBytes(cmd []byte) bool {
	return isMultiKeyCommand(string(cmd))
}

func getKeysForCrossSlotCheck(cmd string, args [][]byte) [][]byte {
	extractor, ok := multiKeyCommands[cmd]
	if !ok {
		return nil
	}
	return extractor(args)
}
