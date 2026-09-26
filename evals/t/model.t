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
    File::Spec->catdir($tree, "home", ".config", "opencode"),
    File::Spec->catdir($tree, "bin"),
);

sub _copy_rel {
    my ($args) = @_;
    my @parts = split m{/}, $args->{rel};
    my $src = File::Spec->catfile($src_evals, @parts);
    my $dst = File::Spec->catfile($dst_evals, @parts);
    File::Copy::copy($src, $dst) or BAIL_OUT("copy $args->{rel}: $!");
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
_copy_rel({ rel => "fixtures/AGENTS.md" });

_write_file({
    path    => File::Spec->catfile($dst_evals, "scenarios", "model-probe.yaml"),
    content => 'id: model-probe
name: model pin resolution probe
tier: both
tags: [probe]
prompt: |
  probe
assert:
  transcript:
    - pattern: "fake opencode finished"
      must: true
timeout_sec: 30
',
});

my $host_raw = '{"provider":{"mimo":{"npm":"@ai-sdk/openai-compatible","name":"MiMo","options":'
    . '{"baseURL":"https://example.invalid/v1","apiKey":"test-key"},"models":'
    . '{"mimo-v2.6-pro":{"name":"mimo-v2.6-pro"}}},'
    . '"anthropic":{"npm":"@ai-sdk/anthropic","name":"Anthropic","options":'
    . '{"baseURL":"https://example.invalid/v2","apiKey":"test-key"},"models":'
    . '{"claude-sonnet-4":{"name":"claude-sonnet-4"}}}}}';
_write_file({
    path    => File::Spec->catfile($tree, "home", ".config", "opencode", "opencode.jsonc"),
    content => $host_raw,
});

my $fake_src = "#!$^X\n" . 'use strict;
use warnings;
use JSON::PP ();
my $argv_file = defined $ENV{FAKE_ARGV} ? $ENV{FAKE_ARGV} : "";
if ($argv_file ne "") {
    if (open my $af, ">", $argv_file) {
        print {$af} JSON::PP::encode_json(\@ARGV);
        close $af;
    }
}
print "fake opencode finished\n";
exit 0;
';
my $fake_path = File::Spec->catfile($tree, "bin", "opencode");
_write_file({ path => $fake_path, content => $fake_src });
chmod 0755, $fake_path or BAIL_OUT("chmod $fake_path: $!");

my $run_pl = File::Spec->catfile($dst_evals, "run.pl");

sub _run_probe {
    my ($args) = @_;
    local $ENV{HOME} = File::Spec->catdir($tree, "home");
    local $ENV{PATH} = File::Spec->catdir($tree, "bin") . ":" . $ENV{PATH};
    local $ENV{KNM_MODEL} = defined $args->{env} ? $args->{env} : "";
    return _spawn_and_capture($args);
}

sub _spawn_and_capture {
    my ($args) = @_;
    local $ENV{FAKE_ARGV} = $args->{argv_file};
    my @cmd = ($^X, $run_pl, "--scenario", "model-probe");
    if (defined $args->{flag}) {
        push @cmd, "--model", $args->{flag};
    }
    my $out = "";
    my $exit = 0;
    if (open my $fh, "-|", @cmd) {
        local $/;
        $out = <$fh> // "";
        close $fh;
        $exit = $?;
    }
    else {
        $exit = -1;
    }
    return ($exit >> 8, $out);
}

sub _spawn_via_shell {
    my ($args) = @_;
    local $ENV{FAKE_ARGV} = $args->{argv_file};
    my $flag_part = defined $args->{flag} ? " --model $args->{flag}" : "";
    my $out = "";
    my $exit = 0;
    if (open my $fh, "-|", "$^X $run_pl --scenario model-probe$flag_part 2>&1") {
        local $/;
        $out = <$fh> // "";
        close $fh;
        $exit = $?;
    }
    else {
        $exit = -1;
    }
    return ($exit >> 8, $out);
}

sub _model_from_argv {
    my ($argv_file) = @_;
    my $raw = "";
    if (open my $af, "<", $argv_file) {
        local $/;
        $raw = <$af> // "";
        close $af;
    }
    else {
        return undef;
    }
    my $argv = JSON::PP::decode_json($raw);
    for my $i (0 .. $#$argv - 1) {
        if ($argv->[$i] eq "--model") {
            return $argv->[$i + 1];
        }
    }
    return undef;
}

my $argv1 = File::Spec->catfile($tree, "argv1.json");
my ($exit1, $out1) = _run_probe({ argv_file => $argv1 });
is($exit1, 0, "default run exits 0");
is(_model_from_argv($argv1), "mimo/mimo-v2.6-pro", "no flag/env uses the default pin");

my $argv2 = File::Spec->catfile($tree, "argv2.json");
my ($exit2, $out2) = _run_probe({ argv_file => $argv2, env => "anthropic/claude-sonnet-4" });
is($exit2, 0, "KNM_MODEL run exits 0");
is(_model_from_argv($argv2), "anthropic/claude-sonnet-4", "KNM_MODEL env sets the pin");

my $argv3 = File::Spec->catfile($tree, "argv3.json");
my ($exit3, $out3) = _run_probe({
    argv_file => $argv3,
    env       => "anthropic/claude-sonnet-4",
    flag      => "mimo/mimo-v2.6-pro",
});
is($exit3, 0, "flag-plus-env run exits 0");
is(_model_from_argv($argv3), "mimo/mimo-v2.6-pro", "--model flag beats KNM_MODEL env");

my $argv4 = File::Spec->catfile($tree, "argv4.json");
my ($exit4, $out4) = _run_probe({ argv_file => $argv4, env => "   " });
is($exit4, 0, "whitespace env run exits 0");
is(_model_from_argv($argv4), "mimo/mimo-v2.6-pro", "whitespace-only KNM_MODEL counts as unset");

my $argv5 = File::Spec->catfile($tree, "argv5.json");
my ($exit5, $out5) = _spawn_via_shell({ argv_file => $argv5, flag => "not-a-model" });
isnt($exit5, 0, "malformed model exits non-zero");
like($out5, qr/model must be in provider\/model form/, "malformed model dies with the shape error");
ok(!-e $argv5, "malformed model dies before spawning opencode");

my $argv5b = File::Spec->catfile($tree, "argv5b.json");
my ($exit5b, $out5b) = _run_probe({
    argv_file => $argv5b,
    env       => "anthropic/claude-sonnet-4",
    flag      => "   ",
});
is($exit5b, 0, "whitespace flag run exits 0");
is(_model_from_argv($argv5b), "anthropic/claude-sonnet-4", "whitespace-only --model falls through to KNM_MODEL");

my $mimo_only = '{"provider":{"mimo":{"npm":"@ai-sdk/openai-compatible","name":"MiMo","options":'
    . '{"baseURL":"https://example.invalid/v1","apiKey":"test-key"},"models":'
    . '{"mimo-v2.6-pro":{"name":"mimo-v2.6-pro"}}}}}';
_write_file({
    path    => File::Spec->catfile($tree, "home", ".config", "opencode", "opencode.jsonc"),
    content => $mimo_only,
});
my $argv6 = File::Spec->catfile($tree, "argv6.json");
my ($exit6, $out6) = _spawn_via_shell({ argv_file => $argv6, flag => "openai/gpt-x" });
isnt($exit6, 0, "absent provider block exits non-zero");
like($out6, qr/need the openai provider block for model openai\/gpt-x/, "absent provider error names the provider key and model");
ok(!-e $argv6, "absent provider block dies before spawning opencode");

done_testing
