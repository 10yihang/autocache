package memory

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/10yihang/autocache/internal/engine"
)

func SerializeEntryValue(entry *engine.Entry) ([]byte, error) {
	if entry == nil {
		return nil, fmt.Errorf("entry is nil")
	}

	var (
		payload []byte
		err     error
	)

	switch entry.Type {
	case engine.TypeString:
		switch value := entry.Value.(type) {
		case []byte:
			payload = append([]byte(nil), value...)
		case string:
			payload = []byte(value)
		default:
			return nil, fmt.Errorf("unsupported string value type %T", entry.Value)
		}
	case engine.TypeHash:
		value, ok := entry.Value.(map[string]string)
		if !ok {
			return nil, fmt.Errorf("unsupported hash value type %T", entry.Value)
		}
		payload, err = json.Marshal(value)
	case engine.TypeList:
		value, ok := entry.Value.([]string)
		if !ok {
			return nil, fmt.Errorf("unsupported list value type %T", entry.Value)
		}
		payload, err = json.Marshal(value)
	case engine.TypeSet:
		switch value := entry.Value.(type) {
		case map[string]struct{}:
			members := make([]string, 0, len(value))
			for member := range value {
				members = append(members, member)
			}
			sort.Strings(members)
			payload, err = json.Marshal(members)
		case []string:
			members := append([]string(nil), value...)
			sort.Strings(members)
			payload, err = json.Marshal(members)
		default:
			return nil, fmt.Errorf("unsupported set value type %T", entry.Value)
		}
	case engine.TypeZSet:
		switch value := entry.Value.(type) {
		case map[string]float64:
			members := make([]engine.ZMember, 0, len(value))
			for member, score := range value {
				members = append(members, engine.ZMember{Member: member, Score: score})
			}
			sort.Slice(members, func(i, j int) bool {
				if members[i].Score == members[j].Score {
					return members[i].Member < members[j].Member
				}
				return members[i].Score < members[j].Score
			})
			payload, err = json.Marshal(members)
		case []engine.ZMember:
			members := append([]engine.ZMember(nil), value...)
			sort.Slice(members, func(i, j int) bool {
				if members[i].Score == members[j].Score {
					return members[i].Member < members[j].Member
				}
				return members[i].Score < members[j].Score
			})
			payload, err = json.Marshal(members)
		default:
			return nil, fmt.Errorf("unsupported zset value type %T", entry.Value)
		}
	default:
		return nil, fmt.Errorf("unsupported value type %d", entry.Type)
	}

	if err != nil {
		return nil, err
	}

	serialized := make([]byte, 1+len(payload))
	serialized[0] = byte(entry.Type)
	copy(serialized[1:], payload)
	return serialized, nil
}

func DecodeSerializedValue(valueType engine.ValueType, payload []byte) (any, error) {
	switch valueType {
	case engine.TypeString:
		return string(payload), nil
	case engine.TypeHash:
		var value map[string]string
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, err
		}
		return value, nil
	case engine.TypeList:
		var value []string
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, err
		}
		return value, nil
	case engine.TypeSet:
		var members []string
		if err := json.Unmarshal(payload, &members); err != nil {
			return nil, err
		}
		value := make(map[string]struct{}, len(members))
		for _, member := range members {
			value[member] = struct{}{}
		}
		return value, nil
	case engine.TypeZSet:
		var members []engine.ZMember
		if err := json.Unmarshal(payload, &members); err != nil {
			return nil, err
		}
		value := make(map[string]float64, len(members))
		for _, member := range members {
			value[member.Member] = member.Score
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported value type %d", valueType)
	}
}
