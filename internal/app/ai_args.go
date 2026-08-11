package app

import "strconv"

func aiArguments(args []string) (Request, bool) {
	if len(args) < 1 || args[0] != "config-review" {
		return Request{}, false
	}
	request := Request{Command: "ai", AIAction: "config-review"}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--config":
			if index+1 >= len(args) || request.ConfigPath != "" || request.ProfileName != "" {
				return Request{}, false
			}
			request.ConfigPath = args[index+1]
			index++
		case "--profile":
			if index+1 >= len(args) || request.ProfileName != "" || request.ConfigPath != "" {
				return Request{}, false
			}
			request.ProfileName = args[index+1]
			index++
		case "--request":
			if index+1 >= len(args) || request.AIRequest != "" {
				return Request{}, false
			}
			request.AIRequest = args[index+1]
			index++
		case "--timeout":
			if index+1 >= len(args) || request.AITimeout != 0 {
				return Request{}, false
			}
			seconds, err := strconv.Atoi(args[index+1])
			if err != nil || seconds < 1 || seconds > 600 {
				return Request{}, false
			}
			request.AITimeout = seconds
			index++
		default:
			return Request{}, false
		}
	}
	if request.ConfigPath == "" && request.ProfileName == "" {
		return Request{}, false
	}
	return request, true
}
