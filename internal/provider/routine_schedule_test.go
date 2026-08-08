package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func weekdayList(t *testing.T, days ...string) types.List {
	t.Helper()
	l, diags := types.ListValueFrom(context.Background(), types.StringType, days)
	if diags.HasError() {
		t.Fatalf("build list: %v", diags)
	}
	return l
}

// Every variant must survive HCL → union → HCL unchanged. This is the whole
// risk of flattening a sum type into a block of optional attributes.
func TestScheduleRoundTripsEveryVariant(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		in   scheduleModel
	}{
		{"manual", scheduleModel{Type: types.StringValue("manual")}},
		{"every", scheduleModel{Type: types.StringValue("every"), IntervalS: types.Int64Value(300)}},
		{"once", scheduleModel{Type: types.StringValue("once"), AtMS: types.Int64Value(1_800_000_000_000)}},
		{"daily", scheduleModel{
			Type: types.StringValue("daily"), Timezone: types.StringValue("Europe/London"),
			Hour: types.Int64Value(9), Minute: types.Int64Value(30),
		}},
		{"weekly", scheduleModel{
			Type: types.StringValue("weekly"), Timezone: types.StringValue("UTC"),
			Hour: types.Int64Value(6), Minute: types.Int64Value(0),
			Weekdays: weekdayList(t, "Mon", "Thu"),
		}},
		{"monthly", scheduleModel{
			Type: types.StringValue("monthly"), Timezone: types.StringValue("UTC"),
			Hour: types.Int64Value(1), Minute: types.Int64Value(15), DayOfMonth: types.Int64Value(28),
		}},
		{"yearly", scheduleModel{
			Type: types.StringValue("yearly"), Timezone: types.StringValue("UTC"),
			Hour: types.Int64Value(0), Minute: types.Int64Value(5),
			Month: types.Int64Value(3), DayOfMonth: types.Int64Value(1),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.in
			union, err := toSchedule(ctx, &input)
			if err != nil {
				t.Fatalf("to union: %v", err)
			}
			back := fromSchedule(ctx, union)
			if back.Type.ValueString() != tc.name {
				t.Fatalf("type = %q, want %q", back.Type.ValueString(), tc.name)
			}
			for _, f := range []struct {
				label     string
				want, got any
			}{
				{"interval_seconds", input.IntervalS, back.IntervalS},
				{"at_ms", input.AtMS, back.AtMS},
				{"timezone", input.Timezone, back.Timezone},
				{"hour", input.Hour, back.Hour},
				{"minute", input.Minute, back.Minute},
				{"day_of_month", input.DayOfMonth, back.DayOfMonth},
				{"month", input.Month, back.Month},
			} {
				if f.want != f.got {
					t.Errorf("%s: got %v, want %v", f.label, f.got, f.want)
				}
			}
			if !input.Weekdays.IsNull() && !input.Weekdays.Equal(back.Weekdays) {
				t.Errorf("weekdays: got %v, want %v", back.Weekdays, input.Weekdays)
			}
		})
	}
}

// An attribute the chosen type does not use is an error, not something quietly
// dropped — "every, with no interval" cannot be expressed on the wire, and the
// mirror of that is "daily, with an interval" being rejected here.
func TestScheduleRejectsAttributesTheTypeDoesNotUse(t *testing.T) {
	_, err := toSchedule(context.Background(), &scheduleModel{
		Type:      types.StringValue("daily"),
		Timezone:  types.StringValue("UTC"),
		Hour:      types.Int64Value(9),
		Minute:    types.Int64Value(0),
		IntervalS: types.Int64Value(60),
	})
	if err == nil {
		t.Fatal("want an error naming interval_seconds")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "interval_seconds") {
		t.Errorf("error should name the stray attribute, got %q", got)
	}
}

func TestScheduleRequiresTheAttributesTheTypeNeeds(t *testing.T) {
	_, err := toSchedule(context.Background(), &scheduleModel{Type: types.StringValue("every")})
	if err == nil || !strings.Contains(err.Error(), "interval_seconds") {
		t.Fatalf("want an error naming the missing attribute, got %v", err)
	}
}

func TestScheduleRejectsAnUnknownType(t *testing.T) {
	_, err := toSchedule(context.Background(), &scheduleModel{Type: types.StringValue("hourly")})
	if err == nil || !strings.Contains(err.Error(), "hourly") {
		t.Fatalf("want an error naming the bad type, got %v", err)
	}
}

// An omitted block is a manual routine, matching horsie's own default.
func TestNilScheduleIsManual(t *testing.T) {
	union, err := toSchedule(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil schedule: %v", err)
	}
	if _, ok := union.Variant.(api.RoutineScheduleManual); !ok {
		t.Errorf("want manual, got %T", union.Variant)
	}
}
