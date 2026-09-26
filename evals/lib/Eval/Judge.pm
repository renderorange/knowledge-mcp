package Eval::Judge;

use strict;
use warnings;
use Carp ();
use JSON::PP ();
use POSIX ();
use Try::Tiny ();

my $_JUDGE_TIMEOUT_SEC = 120;
my $_TRANSCRIPT_MAX_CHARS = 24000;
my $_TRANSCRIPT_HEAD_CHARS = 8000;
my $_TRANSCRIPT_TAIL_CHARS = 16000;

# grade runs one judge call (a single opencode run with the fixed judge
# prompt) against a scenario transcript. The caller passes the resolved
# model so the judge never hardcodes a pin of its own. Returns
# {score, reason, pass}; parse failures die so callers can surface them
# as failing rows instead of treating garbage as a pass.
sub grade {
    my ($class, $sc, $transcript, $opts) = @_;
    if (ref($sc) ne "HASH") {
        Carp::confess("grade requires a scenario hashref");
    }
    if (ref($sc->{judge}) ne "HASH") {
        Carp::confess("grade requires a judge block");
    }
    if (!defined $sc->{judge}{rubric} || "$sc->{judge}{rubric}" !~ /\S/) {
        Carp::confess("grade requires a non-empty judge rubric");
    }
    if (ref($opts) ne "HASH") {
        Carp::confess("grade requires an opts hashref");
    }
    if (!defined $opts->{model} || "$opts->{model}" !~ m{^[^/]+/.+}) {
        Carp::confess("grade requires the resolved provider/model pin");
    }
    my $prompt = _prompt($sc, $transcript);
    my $out = _run_judge($opts->{model}, $opts->{dir}, $prompt);
    my $parsed = parse_response($out);
    my $pass = passes($sc->{judge}, $parsed);
    return {
        score  => $parsed->{score},
        reason => $parsed->{reason},
        pass   => $pass,
    };
}

sub parse_response {
    my ($text) = @_;
    if (!defined $text || "$text" !~ /\S/) {
        Carp::confess("empty judge response");
    }
    my $start = index("$text", "{");
    if ($start < 0) {
        Carp::confess("no JSON in judge response: $text");
    }
    my $data;
    Try::Tiny::try {
        my $decoder = JSON::PP->new;
        $data = $decoder->incr_parse(substr("$text", $start));
    }
    Try::Tiny::catch {
        Carp::confess("judge response is not valid JSON: $_");
    };
    if (!defined $data) {
        Carp::confess("no JSON object in judge response: $text");
    }
    if (ref($data) ne "HASH") {
        Carp::confess("judge response is not a JSON object");
    }
    if (!exists $data->{score}) {
        Carp::confess("judge response missing score");
    }
    if (ref($data->{score})) {
        Carp::confess("judge response score is not an integer: " . JSON::PP::encode_json($data->{score}));
    }
    if ("$data->{score}" !~ /^-?\d+$/) {
        Carp::confess("judge response score is not an integer: $data->{score}");
    }
    $data->{score} = 0 + $data->{score};
    return $data;
}

sub passes {
    my ($judge, $result) = @_;
    my $min = defined $judge->{min} ? 0 + $judge->{min} : 0;
    my $score = defined $result->{score} ? 0 + $result->{score} : -1;
    if ($score >= $min) {
        return 1;
    }
    return 0;
}

sub _prompt {
    my ($sc, $transcript) = @_;
    my $scale = defined $sc->{judge}{scale} ? "$sc->{judge}{scale}" : "0-2";
    return join "\n",
        "You are grading an agent transcript against a rubric.",
        "Rubric: $sc->{judge}{rubric}",
        "Scale: $scale",
        q{Return ONLY JSON of the form {"score": <int>, "reason": "<short>"}.},
        "Transcript:",
        _clip_transcript($transcript);
}

# _clip_transcript bounds the transcript to head+tail with a marker so the
# prompt argument stays well under the kernel's per-argument exec limit
# (MAX_ARG_STRLEN is 128KiB on Linux). Char cap is 24000; worst-case UTF-8
# is 4 bytes/char (~96KiB), leaving headroom for the prompt wrapper.
sub _clip_transcript {
    my ($transcript) = @_;
    my $text = defined $transcript ? "$transcript" : "";
    if (length($text) <= $_TRANSCRIPT_MAX_CHARS) {
        return $text;
    }
    my $head = substr($text, 0, $_TRANSCRIPT_HEAD_CHARS);
    my $tail = substr($text, length($text) - $_TRANSCRIPT_TAIL_CHARS);
    return $head . "\n...[transcript truncated for judge prompt]...\n" . $tail;
}

# _run_judge spawns the judge as a child opencode run and captures its
# stdout only — stderr is UI chrome (spike finding) and must not pollute
# the parse. List-form exec, alarm-bounded, never shell interpolation.
# Child gets its own process group so the timeout kill reaps descendants
# too — a grandchild inheriting the pipe write end would otherwise wedge
# the read past the alarm (same discipline as _spawn_opencode in run.pl).
sub _run_judge {
    my ($model, $dir, $prompt) = @_;
    my $pid = open(my $fh, "-|");
    if (!defined $pid) {
        Carp::confess("fork failed: $!");
    }
    if ($pid == 0) {
        setpgrp(0, 0);
        open(STDIN, "<", "/dev/null");
        open(STDERR, ">", "/dev/null");
        my @cmd = ("opencode", "run");
        if (defined $dir && "$dir" ne "") {
            push @cmd, "--dir", "$dir";
        }
        push @cmd, "--model", "$model", "--format", "default", $prompt;
        exec(@cmd) or POSIX::_exit(127);
    }
    my $timed_out = 0;
    local $SIG{ALRM} = sub {
        $timed_out = 1;
        kill "KILL", $pid;
        kill "KILL", -$pid;
    };
    alarm($_JUDGE_TIMEOUT_SEC);
    my $out = "";
    local $/;
    $out = <$fh> // "";
    close $fh;
    alarm(0);
    if ($timed_out) {
        Carp::confess("judge timed out after ${_JUDGE_TIMEOUT_SEC}s");
    }
    return $out;
}

1;
