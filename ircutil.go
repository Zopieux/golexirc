package main

func emph(word string) string {
	return "\x02\x1f" + word + "\x1f\x02"
}

func green(word string) string {
	return "\x0303" + word + "\x03"
}

func red(word string) string {
	return "\x0304" + word + "\x03"
}

func blue(word string) string {
	return "\x0312" + word + "\x03"
}

func yellow(word string) string {
	return "\x0308" + word + "\x03"
}

func grey(word string) string {
	return "\x0314" + word + "\x03"
}
