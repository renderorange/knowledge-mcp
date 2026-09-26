use strict;
use warnings;
use Test::More;
use FindBin;
use File::Copy ();
use File::Path ();
use File::Spec ();
use File::Temp ();
use JSON::PP ();

my $src_evals = File::Spec->catdir($FindBin::Bin, "..");
my $tree = File::Temp::tempdir(CLEANUP => 1);
my $dst_evals = File::Spec->catdir($tree, "evals");
File::Path::make_path(
    File::Spec->catdir($dst_evals, "lib", "Eval"),
    File::Spec->catdir($dst_evals, "fixtures"),
    File::Spec->catdir($dst_evals, "scenarios"),
    File::Spec->catdir($dst_evals, "mock_agents"),
    File::Spec->catdir($tree, "home", ".config", "opencode"),
    File::Spec->catdir($tree, "bin"),
);

sub _copy_rel {
    my ($args) = @_;
    my @parts = split m{/}, $args->{rel};
    File::Copy::copy(
        File::Spec->catfile($src_evals, @parts),
        File::Spec->catfile($dst_evals, @parts),
    ) or BAIL_OUT("copy $args->{rel}: $!");
    return;
}

sub _write_file {
    my ($args) = @_;
    open my $fh, ">", $args->{path} or BAIL_OUT("write $args->{path}: $!");
    print {$fh} $args->{content};
    close $fh;
    return;
}

_copy_rel({ rel => "run.pl" });
_copy_rel({ rel => "lib/Eval/Assert.pm" });
_copy_rel({ rel => "lib/Eval/Sandbox.pm" });
_copy_rel({ rel => "lib/Eval/Judge.pm" });
_copy_rel({ rel => "fixtures/AGENTS.md" });

my $host_raw = '{"provider":{"mimo":{"npm":"@ai-sdk/openai-compatible","name":"MiMo","options":'
    . '{"baseURL":"https://example.invalid/v1","apiKey":"test-key"},"models":'
    . '{"mimo-v2.6-pro":{"name":"mimo-v2.6-pro"}}}}}';
_write_file({
    path    => File::Spec->catfile($tree, "home", ".config", "opencode", "opencode.jsonc"),
    content => $host_raw,
});

my $fake_src = "#!$^X\n" . 'use strict;
use warnings;
use JSON::PP ();
my $prompt = @ARGV ? "$ARGV[-1]" : "";
my $is_judge = ($prompt =~ /You are grading/) ? 1 : 0;
if (defined $ENV{FAKE_COUNT} && $ENV{FAKE_COUNT} ne "") {
    if (open my $cf, ">>", $ENV{FAKE_COUNT}) {
        print {$cf} $is_judge ? "judge\n" : "main\n";
        close $cf;
    }
}
if ($is_judge) {
    if ((defined $ENV{FAKE_JUDGE} ? $ENV{FAKE_JUDGE} : "") eq "garbage") {
        print "sure, looks fine to me\n";
    }
    else {
        print JSON::PP::encode_json({ score => 2, reason => "compliant" }), "\n";
    }
    exit 0;
}
print "fake opencode finished\n";
my $exit = defined $ENV{FAKE_EXIT} ? 0 + $ENV{FAKE_EXIT} : 0;
exit $exit;
';
my $fake_path = File::Spec->catfile($tree, "bin", "opencode");
_write_file({ path => $fake_path, content => $fake_src });
chmod 0755, $fake_path or BAIL_OUT("chmod $fake_path: $!");

my $run_pl = File::Spec->catfile($dst_evals, "run.pl");

sub _scenarios {
    my ($specs) = @_;
    for my $id (keys %$specs) {
        _write_file({
            path    => File::Spec->catfile($dst_evals, "scenarios", "$id.yaml"),
            content => $specs->{$id},
        });
    }
    return;
}

sub _run {
    my ($args) = @_;
    my $count = File::Spec->catfile($tree, "$args->{id}.count");
    if (-e $count) {
        unlink $count;
    }
    local $ENV{HOME} = File::Spec->catdir($tree, "home");
    local $ENV{PATH} = File::Spec->catdir($tree, "bin") . ":" . $ENV{PATH};
    local $ENV{FAKE_COUNT} = $count;
    local $ENV{FAKE_EXIT} = defined $args->{exit} ? "$args->{exit}" : "";
    local $ENV{FAKE_JUDGE} = defined $args->{judge} ? "$args->{judge}" : "";
    my $out = "";
    my $exit = 0;
    my @cmd = ($^X, $run_pl, "--scenario", $args->{id});
    if ($args->{mock}) {
        push @cmd, "--mock";
    }
    if (open my $fh, "-|", @cmd) {
        local $/;
        $out = <$fh> // "";
        close $fh;
        $exit = $?;
    }
    else {
        $exit = -1;
    }
    my %counts = (main => 0, judge => 0);
    if (open my $cf, "<", $count) {
        while (my $line = <$cf>) {
            chomp $line;
            if (defined $counts{$line}) {
                $counts{$line}++;
            }
        }
        close $cf;
    }
    return ($exit >> 8, $out, \%counts);
}

_scenarios({
    "det-fail" => 'id: det-fail
name: deterministic failure never retries
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "this never appears"
      must: true
retry: 1
timeout_sec: 30
',
    "flake-exit" => 'id: flake-exit
name: opencode exit is flakeable and retries
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "fake opencode finished"
      must: true
retry: 1
timeout_sec: 30
',
    "judge-garbage" => 'id: judge-garbage
name: judge crash is flakeable and retries
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "fake opencode finished"
      must: true
judge:
  rubric: "Did the agent comply?"
  scale: 0-2
  min: 2
retry: 1
timeout_sec: 30
',
    "judge-skip" => 'id: judge-skip
name: judge never runs when deterministic asserts fail
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "this never appears"
      must: true
judge:
  rubric: "Did the agent comply?"
  scale: 0-2
  min: 2
retry: 1
timeout_sec: 30
',
    "judge-ok" => 'id: judge-ok
name: judge passes after deterministic asserts pass
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "fake opencode finished"
      must: true
judge:
  rubric: "Did the agent comply?"
  scale: 0-2
  min: 2
retry: 1
timeout_sec: 30
',
});

my ($exit1, $out1, $c1) = _run({ id => "det-fail" });
isnt($exit1, 0, "deterministic failure exits non-zero");
like($out1, qr/FAIL/, "deterministic failure reports FAIL");
is($c1->{main}, 1, "deterministic assertion failure does not retry");
is($c1->{judge}, 0, "judge never runs on a deterministic failure");

my ($exit2, $out2, $c2) = _run({ id => "flake-exit", exit => 1 });
isnt($exit2, 0, "flake failure exits non-zero");
like($out2, qr/opencode_exit/, "flake failure surfaces the opencode_exit row");
is($c2->{main}, 2, "opencode_exit failure retries the attempt");
is($c2->{judge}, 0, "judge never runs when the run itself failed");

my ($exit3, $out3, $c3) = _run({ id => "judge-garbage", judge => "garbage" });
isnt($exit3, 0, "judge crash exits non-zero");
like($out3, qr/judge error/, "judge crash surfaces a judge error row");
is($c3->{main}, 2, "judge crash retries the attempt");
is($c3->{judge}, 2, "judge crash retries include the judge call");

my ($exit4, $out4, $c4) = _run({ id => "judge-ok" });
is($exit4, 0, "judge pass exits 0");
like($out4, qr/judge score=2 min=2: ok/, "judge row reports score and min");
is($c4->{main}, 1, "passing attempt does not retry");
is($c4->{judge}, 1, "judge runs exactly once after deterministic asserts pass");

my ($exit5, $out5, $c5) = _run({ id => "judge-skip" });
isnt($exit5, 0, "skip case exits non-zero");
is($c5->{judge}, 0, "judge skipped when deterministic asserts fail (case 5)");

_write_file({
    path    => File::Spec->catfile($dst_evals, "mock_agents", "judge-ok.jsonl"),
    content => '{"transcript": "fake opencode finished"}' . "\n" . '{"tool_calls": []}' . "\n"
        . '{"new_files": []}' . "\n" . '{"git_commits": 0}' . "\n",
});
my ($exit6, $out6, $c6) = _run({ id => "judge-ok", mock => 1 });
is($exit6, 0, "mock tier with a judge block still passes");
is($c6->{judge}, 0, "mock tier never runs the judge");
is($c6->{main}, 0, "mock tier never spawns opencode (no retry either)");
unlike($out6, qr/judge score=|judge error/, "mock tier emits no judge rows");

done_testing
