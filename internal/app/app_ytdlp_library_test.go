package app

import (
	"reflect"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestARowsOwnSponsorBlockChoiceWinsOverTheInstances(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Ytdlp.SponsorBlock = ytdlp.SponsorBlock{Remove: []string{"sponsor"}}
	})
	family := putYtdlpFamily(t, a, "https://youtube.com/watch?v=sponsor0001", nil)
	video, audio := family[ytdlp.VariantVideo], family[ytdlp.VariantAudio]

	own := ytdlp.SponsorBlock{Remove: []string{"selfpromo", "chapter"}, Mark: []string{"intro", "ads"}}
	if err := a.SetTaskOptions([]string{video.ID}, TaskOptions{SponsorBlock: &own}); err != nil {
		t.Fatal(err)
	}

	want := ytdlp.SponsorBlock{Remove: []string{"selfpromo"}, Mark: []string{"intro"}}
	if got := a.ytdlpOptionsForTask(video.ID).SponsorBlock; !reflect.DeepEqual(got, want) {
		t.Errorf("the video row asks for %+v, want its own %+v", got, want)
	}
	instance := ytdlp.SponsorBlock{Remove: []string{"sponsor"}, Mark: []string{}}
	if got := a.ytdlpOptionsForTask(audio.ID).SponsorBlock.Sanitize(); !reflect.DeepEqual(got, instance) {
		t.Errorf("the audio row asks for %+v, want the instance's %+v", got, instance)
	}
}

func TestAnEmptySponsorBlockChoiceTurnsTheInstancesOffForTheRow(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Ytdlp.SponsorBlock = ytdlp.SponsorBlock{Remove: []string{"sponsor"}}
	})
	video := putYtdlpFamily(t, a, "https://youtube.com/watch?v=sponsor0002", nil)[ytdlp.VariantVideo]

	if err := a.SetTaskOptions([]string{video.ID}, TaskOptions{SponsorBlock: &ytdlp.SponsorBlock{}}); err != nil {
		t.Fatal(err)
	}
	if got := a.ytdlpOptionsForTask(video.ID).SponsorBlock.Sanitize(); len(got.Remove) != 0 || len(got.Mark) != 0 {
		t.Errorf("the row still asks for %+v", got)
	}
}
