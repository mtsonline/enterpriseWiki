package content

// TimeFields is the common set of fields of every kind of data about a
// point in time: tasks, dates in the text and entries of calendars use
// the same keys and columns, so one report or calendar reads them all
// with one query. states are the values of "state" (the first is the
// default, e.g. open/done or planned/cancelled).
func TimeFields(states []string, stateLabels map[string]map[string]string) []DataField {
	lm := func(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }
	return []DataField{
		{Key: "title", Type: DataText, Max: 300, Column: ColLabel, Labels: lm("Title", "Titel")},
		{Key: "day", Type: DataDay, Column: ColDay, Labels: lm("Day", "Tag")},
		{Key: "at", Type: DataTime, Column: ColAt, Labels: lm("Starts at", "Beginn")},
		{Key: "end", Type: DataText, Max: 40, Labels: lm("Ends", "Ende")},
		{Key: "state", Type: DataChoice, Options: states, OptionLabels: stateLabels, Column: ColState, Required: true, Labels: lm("Status", "Status")},
		{Key: "person", Type: DataPerson, Column: ColPerson, Labels: lm("Person", "Person")},
		{Key: "rrule", Type: DataText, Max: 500, Labels: lm("Repeats", "Wiederholung")},
		{Key: "ref", Type: DataPage, Column: ColRef, Labels: lm("Page", "Seite")},
	}
}
