package bot

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"UEPB/internal/i18n"

	"github.com/PuerkitoBio/goquery"
	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// EventData stores event info
type EventData struct{ Day, Month, Time, Category, Title, Description string }

// GetEventID returns hashed id
func (e EventData) GetEventID() string {
	fullID := fmt.Sprintf("%s_%s_%s", e.Day, e.Month, e.Title)
	hash := md5.Sum([]byte(fullID))
	return hex.EncodeToString(hash[:])[:16]
}

// Month mapping structures
type polishMonth struct {
	normalized string
	timeMonth  time.Month
}

// polishMonths maps month forms
var polishMonths = map[string]polishMonth{"stycznia": {"stycznia", time.January}, "styczeń": {"stycznia", time.January}, "styczen": {"stycznia", time.January}, "lutego": {"lutego", time.February}, "luty": {"lutego", time.February}, "marca": {"marca", time.March}, "marzec": {"marca", time.March}, "kwietnia": {"kwietnia", time.April}, "kwiecień": {"kwietnia", time.April}, "kwiecien": {"kwietnia", time.April}, "maja": {"maja", time.May}, "maj": {"maja", time.May}, "czerwca": {"czerwca", time.June}, "czerwiec": {"czerwca", time.June}, "lipca": {"lipca", time.July}, "lipiec": {"lipca", time.July}, "sierpnia": {"sierpnia", time.August}, "sierpień": {"sierpnia", time.August}, "sierpien": {"sierpnia", time.August}, "września": {"września", time.September}, "wrzesień": {"września", time.September}, "wrzesien": {"września", time.September}, "października": {"października", time.October}, "październik": {"października", time.October}, "pazdziernik": {"października", time.October}, "listopada": {"listopada", time.November}, "listopad": {"listopada", time.November}, "grudnia": {"grudnia", time.December}, "grudzień": {"grudnia", time.December}, "grudzien": {"grudnia", time.December}}

// escapeMarkdown escapes telegram markdown
func escapeMarkdown(text string) string {
	r := strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "`", "\\`")
	return r.Replace(text)
}

// normalizeMonthName standardizes month name
func normalizeMonthName(monthStr string) string {
	m := strings.TrimSpace(monthStr)
	if strings.Contains(m, " ") {
		m = strings.Split(m, " ")[0]
	}
	m = strings.ToLower(m)
	if md, ok := polishMonths[m]; ok {
		return md.normalized
	}
	return m
}

// parseMonthToTime returns time.Month
func parseMonthToTime(monthStr string) (time.Month, bool) {
	m := strings.TrimSpace(monthStr)
	if strings.Contains(m, " ") {
		m = strings.Split(m, " ")[0]
	}
	m = strings.ToLower(m)
	if md, ok := polishMonths[m]; ok {
		return md.timeMonth, true
	}
	return 0, false
}

// fetchEventsFromWebsite scrapes events page
func (fh *FeatureHandler) fetchEventsFromWebsite() error {
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	url := "https://ue.poznan.pl/wydarzenia/"
	resp, err := client.Get(url)
	if err != nil {
		logrus.WithError(err).WithField("url", url).Error("fetch events failed")
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}
	currentMonth := strings.TrimSpace(doc.Find(".eventsList__monthTitle").First().Text())
	var events []EventData
	doc.Find(".eventsList__event").Each(func(_ int, s *goquery.Selection) {
		day := strings.TrimSpace(s.Find(".eventsList__eventDay").Text())
		eventTime := strings.TrimSpace(s.Find(".eventsList__eventTime").Text())
		category := strings.TrimSpace(s.Find(".eventsList__eventCategory").Text())
		title := strings.TrimSpace(s.Find(".eventsList__eventTitle").Text())
		fullText := strings.TrimSpace(s.Find(".eventsList__eventFullText .wysiwyg").Text())
		if fullText == "" {
			fullText = strings.TrimSpace(s.Find(".eventsList__eventExcerpt").Text())
		}
		if title != "" {
			events = append(events, EventData{Day: day, Month: currentMonth, Time: eventTime, Category: category, Title: title, Description: fullText})
		}
	})
	fh.eventsCacheMu.Lock()
	fh.eventsCache = events
	fh.cacheTime = time.Now()
	fh.eventsCacheMu.Unlock()
	logrus.WithField("count", len(events)).Info("Events cached")
	return nil
}

// formatEventText formats one event
func (fh *FeatureHandler) formatEventText(event EventData, index, total int, user *tb.User) string {
	lang := fh.getLangForUser(user)
	msgs := i18n.Get().T(lang)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📰 %s\n\n", escapeMarkdown(event.Title)))
	if event.Description != "" {
		desc := strings.ReplaceAll(event.Description, "\n\n\n", "\n\n")
		desc = strings.TrimSpace(desc)
		trainingScheduleURL := "https://app.ue.poznan.pl/TrainingsSchedule/Account/Login?ReturnUrl=%2fTrainingsSchedule%2f"
		lines := strings.Split(desc, "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			if trim == "Więcej informacji" {
				lines[i] = fmt.Sprintf("[Więcej informacji](%s)", trainingScheduleURL)
			} else {
				lines[i] = escapeMarkdown(line)
			}
		}
		desc = strings.Join(lines, "\n")
		b.WriteString(fmt.Sprintf("%s\n\n", desc))
	}
	if event.Day != "" {
		timeStr := ""
		if event.Time != "" {
			timeStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(event.Time), "-"))
		}
		nm := normalizeMonthName(event.Month)
		if timeStr != "" {
			b.WriteString(fmt.Sprintf(msgs.Events.WillHappen, escapeMarkdown(event.Day), escapeMarkdown(nm), escapeMarkdown(timeStr)))
		} else {
			b.WriteString(fmt.Sprintf(msgs.Events.WillHappenNoTime, escapeMarkdown(event.Day), escapeMarkdown(nm)))
		}
	}
	b.WriteString(fmt.Sprintf("\n\n"+msgs.Events.EventNumber, index+1, total))
	return b.String()
}

// HandleEvent handles /events in private chat
func (fh *FeatureHandler) HandleEvent(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Chat().Type != tb.ChatPrivate {
		warnMsg, _ := fh.bot.Send(c.Chat(), msgs.Events.PrivateOnly)
		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
		}
		return nil
	}
	fh.eventRateLimitMu.Lock()
	last, exists := fh.eventRateLimit[c.Sender().ID]
	now := time.Now()
	if exists && now.Sub(last) < 30*time.Second {
		remaining := 30*time.Second - now.Sub(last)
		fh.eventRateLimitMu.Unlock()
		_, _ = fh.bot.Send(c.Chat(), fmt.Sprintf(msgs.Events.RateLimit, int(remaining.Seconds())))
		return nil
	}
	fh.eventRateLimit[c.Sender().ID] = now
	fh.eventRateLimitMu.Unlock()
	statusMsg, _ := fh.bot.Send(c.Chat(), msgs.Events.Loading)
	fh.eventsCacheMu.RLock()
	cacheValid := time.Since(fh.cacheTime) < 5*time.Minute && len(fh.eventsCache) > 0
	fh.eventsCacheMu.RUnlock()
	if !cacheValid {
		if err := fh.fetchEventsFromWebsite(); err != nil {
			fh.bot.Edit(statusMsg, msgs.Events.ErrorLoading)
			return nil
		}
	}
	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()
	if len(fh.eventsCache) == 0 {
		fh.bot.Edit(statusMsg, msgs.Events.NoEvents)
		return nil
	}
	event := fh.eventsCache[0]
	eventText := fh.formatEventText(event, 0, len(fh.eventsCache), c.Sender())
	nextBtn := tb.InlineButton{Unique: "next_event", Text: msgs.Buttons.Next, Data: fmt.Sprintf("nav_%d", 0)}
	interestedBtn := tb.InlineButton{Unique: "event_interested", Text: msgs.Buttons.Interested, Data: fmt.Sprintf("int_%s", event.GetEventID())}
	markup := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{{nextBtn}, {interestedBtn}}}
	editedMsg, err := fh.bot.Edit(statusMsg, eventText, markup, tb.ModeMarkdown)
	if err != nil {
		editedMsg, err = fh.bot.Edit(statusMsg, eventText, tb.ModeMarkdown)
		if err != nil {
			return nil
		}
	}
	if editedMsg != nil {
		key := fmt.Sprintf("%d_%d", editedMsg.Chat.ID, editedMsg.ID)
		fh.eventMessageOwnersMu.Lock()
		fh.eventMessageOwners[key] = c.Sender().ID
		fh.eventMessageOwnersMu.Unlock()
	}
	logrus.WithFields(logrus.Fields{"user": fh.adminHandler.GetUserDisplayName(c.Sender()), "event_index": 0, "total": len(fh.eventsCache)}).Info("Event shown")
	return nil
}

// HandlePrevEvent goes to the previous event
func (fh *FeatureHandler) HandlePrevEvent(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}
	key := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	owner, ok := fh.eventMessageOwners[key]
	fh.eventMessageOwnersMu.RUnlock()
	if ok && owner != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Buttons.NotYourButton})
	}
	data := c.Callback().Data
	var idx int
	_, err := fmt.Sscanf(data, "nav_%d", &idx)
	if err != nil {
		return nil
	}
	prev := idx - 1
	if prev < 0 {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.FirstEvent})
	}
	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()
	if prev >= len(fh.eventsCache) {
		return nil
	}
	event := fh.eventsCache[prev]
	text := fh.formatEventText(event, prev, len(fh.eventsCache), c.Sender())
	var navButtons []tb.InlineButton
	if prev > 0 {
		navButtons = append(navButtons, tb.InlineButton{Unique: "prev_event", Text: msgs.Buttons.Prev, Data: fmt.Sprintf("nav_%d", prev)})
	}
	if prev < len(fh.eventsCache)-1 {
		navButtons = append(navButtons, tb.InlineButton{Unique: "next_event", Text: msgs.Buttons.Next, Data: fmt.Sprintf("nav_%d", prev)})
	}
	interested := tb.InlineButton{Unique: "event_interested", Text: msgs.Buttons.Interested, Data: fmt.Sprintf("int_%s", event.GetEventID())}
	markup := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{navButtons, {interested}}}
	_, _ = fh.bot.Edit(c.Callback().Message, text, markup, tb.ModeMarkdown)
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleNextEvent goes to the next event
func (fh *FeatureHandler) HandleNextEvent(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}
	key := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	owner, ok := fh.eventMessageOwners[key]
	fh.eventMessageOwnersMu.RUnlock()
	if ok && owner != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Buttons.NotYourButton})
	}
	data := c.Callback().Data
	var idx int
	_, err := fmt.Sscanf(data, "nav_%d", &idx)
	if err != nil {
		return nil
	}
	next := idx + 1
	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()
	if next >= len(fh.eventsCache) {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.LastEvent})
	}
	event := fh.eventsCache[next]
	text := fh.formatEventText(event, next, len(fh.eventsCache), c.Sender())
	var navButtons []tb.InlineButton
	if next > 0 {
		navButtons = append(navButtons, tb.InlineButton{Unique: "prev_event", Text: msgs.Buttons.Prev, Data: fmt.Sprintf("nav_%d", next)})
	}
	if next < len(fh.eventsCache)-1 {
		navButtons = append(navButtons, tb.InlineButton{Unique: "next_event", Text: msgs.Buttons.Next, Data: fmt.Sprintf("nav_%d", next)})
	}
	interested := tb.InlineButton{Unique: "event_interested", Text: msgs.Buttons.Interested, Data: fmt.Sprintf("int_%s", event.GetEventID())}
	markup := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{navButtons, {interested}}}
	_, _ = fh.bot.Edit(c.Callback().Message, text, markup, tb.ModeMarkdown)
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleEventInterested user wants reminders
func (fh *FeatureHandler) HandleEventInterested(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}
	key := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	owner, ok := fh.eventMessageOwners[key]
	fh.eventMessageOwnersMu.RUnlock()
	if ok && owner != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Buttons.NotYourButton})
	}
	eventID := strings.TrimPrefix(c.Callback().Data, "int_")
	uid := c.Sender().ID
	fh.userEventInterestsMu.RLock()
	userInterests, exists := fh.userEventInterests[uid]
	already := exists && userInterests[eventID]
	fh.userEventInterestsMu.RUnlock()
	if already {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.AlreadySubscribed})
	}
	fh.eventsCacheMu.RLock()
	var current *EventData
	for i := range fh.eventsCache {
		if fh.eventsCache[i].GetEventID() == eventID {
			current = &fh.eventsCache[i]
			break
		}
	}
	fh.eventsCacheMu.RUnlock()
	fh.eventInterestsMu.Lock()
	if fh.eventInterests[eventID] == nil {
		fh.eventInterests[eventID] = []int64{}
	}
	fh.eventInterests[eventID] = append(fh.eventInterests[eventID], uid)
	fh.eventInterestsMu.Unlock()
	fh.userEventInterestsMu.Lock()
	if fh.userEventInterests[uid] == nil {
		fh.userEventInterests[uid] = make(map[string]bool)
	}
	fh.userEventInterests[uid][eventID] = true
	fh.userEventInterestsMu.Unlock()
	logrus.WithFields(logrus.Fields{"user_id": uid, "event_id": eventID}).Info("User subscribed")
	if current != nil {
		timeInfo := ""
		if current.Day != "" && current.Month != "" {
			monthName := current.Month
			if strings.Contains(monthName, " ") {
				monthName = strings.ToLower(strings.Split(monthName, " ")[0])
			}
			if current.Time != "" {
				ts := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(current.Time), "-"))
				timeInfo = fmt.Sprintf("%s %s в %s", current.Day, monthName, ts)
			} else {
				timeInfo = fmt.Sprintf("%s %s", current.Day, monthName)
			}
		}
		unsub := tb.InlineButton{Unique: "event_unsubscribe", Text: msgs.Buttons.Unsubscribe, Data: fmt.Sprintf("unsub_%s", eventID)}
		markup := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{{unsub}}}
		confirm := fmt.Sprintf(msgs.Events.Subscribed, current.Title, timeInfo)
		_, _ = fh.bot.Send(c.Chat(), confirm, markup)
	}
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleEventUnsubscribe removes subscription
func (fh *FeatureHandler) HandleEventUnsubscribe(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Callback() == nil || c.Sender() == nil {
		return nil
	}
	eventID := strings.TrimPrefix(c.Callback().Data, "unsub_")
	uid := c.Sender().ID
	fh.userEventInterestsMu.RLock()
	userInterests, exists := fh.userEventInterests[uid]
	subscribed := exists && userInterests[eventID]
	fh.userEventInterestsMu.RUnlock()
	if !subscribed {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.NotSubscribed})
	}
	fh.eventInterestsMu.Lock()
	if users, ok := fh.eventInterests[eventID]; ok {
		filtered := []int64{}
		for _, u := range users {
			if u != uid {
				filtered = append(filtered, u)
			}
		}
		fh.eventInterests[eventID] = filtered
	}
	fh.eventInterestsMu.Unlock()
	fh.userEventInterestsMu.Lock()
	if m, ok := fh.userEventInterests[uid]; ok {
		delete(m, eventID)
	}
	fh.userEventInterestsMu.Unlock()
	fh.bot.Edit(c.Callback().Message, msgs.Events.Unsubscribed)
	logrus.WithFields(logrus.Fields{"user_id": uid, "event_id": eventID}).Info("User unsubscribed")
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.UnsubscribedCallback})
}

// HandleBroadcastInterested informs user in group broadcast
func (fh *FeatureHandler) HandleBroadcastInterested(c tb.Context) error {
	lang := fh.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{Text: msgs.Events.UsePrivate, ShowAlert: true})
}

// StartEventBroadcaster launches ticker
func (fh *FeatureHandler) StartEventBroadcaster() {
	ticker := time.NewTicker(1 * time.Hour)
	go func() { fh.logUpcomingEvents(); fh.checkAndBroadcastEvents() }()
	go func() {
		for range ticker.C {
			fh.checkAndBroadcastEvents()
		}
	}()
	logrus.Info("Event broadcaster started")
}

// logUpcomingEvents logs next events
func (fh *FeatureHandler) logUpcomingEvents() {
	if err := fh.fetchEventsFromWebsite(); err != nil {
		logrus.WithError(err).Error("initial events fetch failed")
		return
	}
	fh.eventsCacheMu.RLock()
	events := fh.eventsCache
	fh.eventsCacheMu.RUnlock()
	if len(events) == 0 {
		logrus.Info("No events found")
		return
	}
	logrus.Info("UPCOMING EVENTS OVERVIEW:")
	now := time.Now()
	show := 10
	if len(events) < show {
		show = len(events)
	}
	for i, event := range events[:show] {
		date := fh.parseEventDate(event)
		info := fmt.Sprintf("Event %d: %s", i+1, event.Title)
		if !date.IsZero() {
			days := int(date.Sub(now).Hours() / 24)
			bDate := date.AddDate(0, 0, -5)
			if days >= 5 {
				info += fmt.Sprintf(" | Date: %s (%d days) | Will broadcast: %s", date.Format("2006-01-02"), days, bDate.Format("2006-01-02 15:04"))
			} else if days >= 0 {
				info += fmt.Sprintf(" | Date: %s | Too close", date.Format("2006-01-02"))
			} else {
				info += " | Event passed"
			}
		} else {
			info += " | Date: Unknown"
		}
		logrus.Info(info)
	}
	fh.registeredGroupsMu.RLock()
	gc := len(fh.registeredGroups)
	fh.registeredGroupsMu.RUnlock()
	logrus.Infof("Total registered groups: %d", gc)
	logrus.Info("EVENTS IN NEXT 5 DAYS:")
	for i := 1; i <= 5; i++ {
		target := now.AddDate(0, 0, i)
		dayEvents := []string{}
		for _, ev := range events {
			date := fh.parseEventDate(ev)
			if !date.IsZero() && date.Year() == target.Year() && date.Month() == target.Month() && date.Day() == target.Day() {
				dayEvents = append(dayEvents, ev.Title)
			}
		}
		if len(dayEvents) > 0 {
			logrus.Infof("Day +%d (%s): %d events - %s", i, target.Format("2006-01-02"), len(dayEvents), strings.Join(dayEvents, ", "))
		} else {
			logrus.Infof("Day +%d (%s): No events", i, target.Format("2006-01-02"))
		}
	}
}

// checkAndBroadcastEvents sends broadcasts 5 days before
func (fh *FeatureHandler) checkAndBroadcastEvents() {
	lang := i18n.Get().GetDefault()
	msgs := i18n.Get().T(lang)

	if err := fh.fetchEventsFromWebsite(); err != nil {
		logrus.WithError(err).Error("broadcast fetch failed")
		return
	}
	fh.eventsCacheMu.RLock()
	events := fh.eventsCache
	fh.eventsCacheMu.RUnlock()
	if len(events) == 0 {
		return
	}
	target := time.Now().AddDate(0, 0, 5)
	for _, event := range events {
		date := fh.parseEventDate(event)
		if date.IsZero() {
			continue
		}
		if date.Year() == target.Year() && date.Month() == target.Month() && date.Day() == target.Day() {
			id := event.GetEventID()
			fh.broadcastedEventsMu.Lock()
			if fh.broadcastedEvents[id] {
				fh.broadcastedEventsMu.Unlock()
				continue
			}
			fh.broadcastedEvents[id] = true
			fh.broadcastedEventsMu.Unlock()
			fh.registeredGroupsMu.RLock()
			groups := make([]int64, 0, len(fh.registeredGroups))
			for cid := range fh.registeredGroups {
				groups = append(groups, cid)
			}
			fh.registeredGroupsMu.RUnlock()
			for _, cid := range groups {
				chat := &tb.Chat{ID: cid}
				text := fmt.Sprintf(msgs.Events.BroadcastReminder, event.Title, event.Day, event.Month)
				if event.Time != "" {
					text = fmt.Sprintf(msgs.Events.BroadcastReminder, event.Title, event.Day, event.Month) + fmt.Sprintf(" в %s", event.Time)
				}
				text += msgs.Events.BroadcastDetails
				if _, err := fh.bot.Send(chat, text); err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{"chat_id": cid, "event_id": id}).Error("broadcast failed")
				} else {
					logrus.WithFields(logrus.Fields{"chat_id": cid, "event_id": id}).Info("Event broadcasted")
				}
			}
		}
	}
}

// parseEventDate parses day+month to date (assumes current or next year)
func (fh *FeatureHandler) parseEventDate(event EventData) time.Time {
	if event.Day == "" || event.Month == "" {
		return time.Time{}
	}
	dayStr := strings.TrimSpace(event.Day)
	var day int
	if _, err := fmt.Sscanf(dayStr, "%d", &day); err != nil {
		return time.Time{}
	}
	month, ok := parseMonthToTime(strings.TrimSpace(event.Month))
	if !ok {
		return time.Time{}
	}
	now := time.Now()
	year := now.Year()
	if month < now.Month() {
		year++
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
