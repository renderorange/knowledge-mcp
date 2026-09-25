use strict;
use warnings;
use Carp ();
use FindBin;
use lib "$FindBin::Bin/lib";
use Getopt::Long ();
use JSON::PP ();
use POSIX ();
use Try::Tiny ();
use YAML::PP ();
use File::Find ();
use Eval::Sandbox;
use Eval::Assert;

my ($want_scenario, $want_tag, $xml_path, $mock);
Getopt::Long::GetOptions(
    "scenario=s" => \$want_scenario,
    "tag=s"      => \$want_tag,
    "xml=s"      => \$xml_path,
    "mock"       => \$mock,
) or Carp::confess("bad options");

if (!$mock) {
    my $found = system("command -v opencode >/dev/null 2>&1");
    if ($found != 0) {
        Carp::confess("opencode binary not found; real-tier evals require it (no silent skip)");
    }
}

my $bin = $ENV{KNM_BIN} || "knowledge-mcp";

my @scenarios;
for my $f (glob "$FindBin::Bin/scenarios/*.yaml") {
    my $sc;
    Try::Tiny::try {
        $sc = YAML::PP->new->load_file($f);
    }
    Try::Tiny::catch {
        Carp::confess("failed to load $f: $_");
    };
    if ($want_scenario && $sc->{id} ne $want_scenario) {
        next;
    }
    if ($want_tag && !grep { $_ eq $want_tag } @{ $sc->{tags} || [] }) {
        next;
    }
    if (($sc->{tier} || "") eq "mock") {
        next;
    }
    push @scenarios, { %$sc, _file => $f };
}

if (!@scenarios) {
    print "no scenarios matched filters\n";
}

my @results;
for my $sc (@scenarios) {
    push @results, run_one($sc, $bin, $mock ? 1 : 0);
}

report(\@results, $xml_path);
my $failed = grep { $_->{failed} } @results;
exit($failed ? 1 : 0);

sub run_one {
    my ($sc, $bin, $mock) = @_;
    my $attempts = 1 + ($sc->{retry} || 0);
    my $last;
    for my $try (1 .. $attempts) {
        if ($mock) {
            $last = mock_attempt($sc, $bin);
        }
        else {
            $last = attempt($sc, $bin);
        }
        if (!$last->{failed}) {
            last;
        }
    }
    return $last;
}

sub attempt {
    my ($sc, $bin) = @_;
    my $sb = Eval::Sandbox->build($sc, {
        bin          => $bin,
        make_git     => 1,
        fixtures_dir => "$FindBin::Bin/fixtures",
    });
    if (!$sb->{provider_lifted}) {
        $sb->teardown();
        Carp::confess("real-tier evals need the mimo provider block in the host opencode config " .
            "(default ~/.config/opencode/opencode.jsonc)");
    }
    local $ENV{HOME} = $sb->{home};
    local $ENV{OPENCODE_CONFIG} = "$sb->{home}/.config/opencode/opencode.jsonc";

    my ($exit, $timed_out, $signal) = _spawn_opencode($sb, $sc->{prompt});

    my $transcript = "";
    if (open my $fh, "<", $sb->{transcript}) {
        local $/;
        $transcript = <$fh> // "";
        close $fh;
    }
    my ($tool_calls, $tool_err) = _tool_calls($sb);
    my ($new_files, $new_err) = _new_files($sb);
    my %ev = (
        transcript  => $transcript,
        tool_calls  => $tool_calls,
        new_files   => $new_files,
        git_commits => _git_commits($sb),
        root        => $sb->{project},
    );
    if (defined $new_err) {
        $ev{new_files} = undef;
        $ev{new_files_error} = $new_err;
    }
    my @checks = @{ Eval::Assert::check($sc, \%ev) };
    if (defined $tool_err) {
        push @checks, {
            name   => "tool_log",
            pass   => 0,
            detail => "tool_call evidence collection failed: $tool_err",
        };
    }
    if ($timed_out) {
        push @checks, {
            name   => "runner_timeout",
            pass   => 0,
            detail => "killed after $sb->{timeout_sec}s (runner-side timeout; not an opencode exit code)",
        };
    }
    elsif ($exit != 0 || $signal != 0) {
        my $detail = "exit=$exit";
        if ($signal != 0) {
            $detail = "exit=$exit signal=$signal";
        }
        if ($transcript =~ /try again in 15 minutes|Model use case details have not been submitted/) {
            $detail .= "; infra flake (bedrock model error), not a scenario regression";
        }
        push @checks, { name => "opencode_exit", pass => 0, detail => $detail };
    }
    my $failed = grep { !$_->{pass} } @checks;
    $sb->teardown();
    return { id => $sc->{id}, checks => \@checks, failed => $failed };
}

sub mock_attempt {
    my ($sc, $bin) = @_;
    my $path = "$FindBin::Bin/mock_agents/$sc->{id}.jsonl";
    if (!-e $path) {
        return {
            id      => $sc->{id},
            checks  => [ { name => "mock_evidence", pass => 0, detail => "missing $path" } ],
            failed  => 1,
        };
    }
    my $me = _load_mock_evidence($path);
    my $sb = Eval::Sandbox->build($sc, {
        bin          => $bin,
        make_git     => 0,
        fixtures_dir => "$FindBin::Bin/fixtures",
    });
    for my $rel (@{ $me->{new_files} }) {
        $sb->write_file({ path => $rel, content => "mock" });
    }
    my %ev = (
        transcript  => $me->{transcript},
        tool_calls  => $me->{tool_calls},
        new_files   => $me->{new_files},
        git_commits => $me->{git_commits},
        root        => $sb->{project},
    );
    my $checks = Eval::Assert::check($sc, \%ev);
    my $failed = grep { !$_->{pass} } @$checks;
    $sb->teardown();
    return { id => $sc->{id}, checks => $checks, failed => $failed };
}

sub _spawn_opencode {
    my ($sb, $prompt) = @_;
    my $pid = fork();
    if (!defined $pid) {
        Carp::confess("fork failed: $!");
    }
    if ($pid == 0) {
        setpgrp(0, 0);
        open(STDIN, "<", "/dev/null");
        open(STDOUT, ">", $sb->{transcript});
        open(STDERR, ">&", \*STDOUT);
        exec("opencode", "run",
            "--dir", $sb->{project},
            "--model", "mimo/mimo-v2.6-pro",
            "--format", "default",
            $prompt,
        ) or POSIX::_exit(127);
    }
    my $timed_out = 0;
    local $SIG{ALRM} = sub {
        $timed_out = 1;
        kill "KILL", $pid;
        kill "KILL", -$pid;
    };
    alarm($sb->{timeout_sec});
    my $reaped = waitpid($pid, 0);
    if ($reaped == -1) {
        $reaped = waitpid($pid, 0);
    }
    alarm(0);
    my $status = $?;
    return ($status >> 8, $timed_out, $status & 127);
}

sub _tool_calls {
    my ($sb) = @_;
    my $path = $sb->{tool_log};
    if (!-e $path) {
        return ([], undef);
    }
    my @calls;
    my $err;
    my $n = 0;
    if (open my $fh, "<", $path) {
        while (my $line = <$fh>) {
            $n++;
            if ($line !~ /\S/) {
                next;
            }
            my $row;
            Try::Tiny::try {
                $row = JSON::PP::decode_json($line);
            }
            Try::Tiny::catch {
                $err = "$path:$n: $_";
            };
            if (defined $err) {
                last;
            }
            push @calls, $row;
        }
        close $fh;
    }
    else {
        return (undef, "read $path: $!");
    }
    if (defined $err) {
        return ([], $err);
    }
    return (\@calls, undef);
}

sub _new_files {
    my ($sb) = @_;
    my $root = $sb->{project};
    my $before = $sb->{manifest_before};
    if (ref($before) ne "HASH") {
        return (undef, "manifest_before missing from sandbox");
    }
    my @new;
    my $err;
    Try::Tiny::try {
        File::Find::find(sub {
            if (-d $_) {
                return;
            }
            my $rel = $File::Find::name;
            $rel =~ s{^\Q$root\E/}{};
            if (index($rel, ".git/") == 0) {
                return;
            }
            if (!$before->{$rel}) {
                push @new, $rel;
            }
        }, $root);
    }
    Try::Tiny::catch {
        $err = "new-file walk failed: $_";
    };
    if (defined $err) {
        return (undef, $err);
    }
    return (\@new, undef);
}

sub _git_commits {
    my ($sb) = @_;
    my $pid = open(my $fh, "-|");
    if (!defined $pid) {
        return 0;
    }
    if ($pid == 0) {
        open(STDERR, ">", "/dev/null");
        exec("git", "-C", $sb->{project}, "rev-list", "--count", "HEAD") or POSIX::_exit(127);
    }
    my $count = "";
    local $/;
    $count = <$fh> // "";
    close $fh;
    if ($? != 0) {
        return 0;
    }
    chomp $count;
    return $count ? 0 + $count : 0;
}

sub _load_mock_evidence {
    my ($path) = @_;
    my %ev = (
        transcript  => "",
        tool_calls  => [],
        new_files   => [],
        git_commits => 0,
    );
    if (open my $fh, "<", $path) {
        my $n = 0;
        while (my $line = <$fh>) {
            $n++;
            if ($line !~ /\S/) {
                next;
            }
            my $row;
            Try::Tiny::try {
                $row = JSON::PP::decode_json($line);
            }
            Try::Tiny::catch {
                Carp::confess("$path:$n: $_");
            };
            if (ref($row) ne "HASH") {
                Carp::confess("$path:$n: row is not a JSON object");
            }
            if (defined $row->{transcript}) {
                $ev{transcript} .= $row->{transcript} . "\n";
            }
            if (ref($row->{tool_calls}) eq "ARRAY") {
                push @{ $ev{tool_calls} }, @{ $row->{tool_calls} };
            }
            if (ref($row->{new_files}) eq "ARRAY") {
                push @{ $ev{new_files} }, @{ $row->{new_files} };
            }
            if (defined $row->{git_commits}) {
                $ev{git_commits} = 0 + $row->{git_commits};
            }
        }
        close $fh;
    }
    else {
        Carp::confess("read $path: $!");
    }
    return \%ev;
}

sub report {
    my ($results, $xml) = @_;
    for my $r (@$results) {
        my $status = $r->{failed} ? "FAIL" : "PASS";
        print "[$status] $r->{id}\n";
        for my $c (@{ $r->{checks} || [] }) {
            if ($c->{pass}) {
                print "  - $c->{name}: ok\n";
            }
            else {
                print "  - $c->{name}: FAIL ($c->{detail})\n";
            }
        }
    }
    if ($xml) {
        open my $fh, ">", $xml or Carp::confess("write $xml: $!");
        print {$fh} qq{<?xml version="1.0"?><testsuites>};
        for my $r (@$results) {
            my $cases = join "", map { _xml_case($_) } @{ $r->{checks} || [] };
            print {$fh} "<testsuite name=\"" . _xml_escape($r->{id}) . "\">$cases</testsuite>";
        }
        print {$fh} "</testsuites>";
        close $fh;
    }
    return;
}

sub _xml_case {
    my ($c) = @_;
    my $name = _xml_escape($c->{name});
    if ($c->{pass}) {
        return "<testcase name=\"$name\"/>";
    }
    return "<testcase name=\"$name\"><failure>" . _xml_escape($c->{detail}) . "</failure></testcase>";
}

sub _xml_escape {
    my ($s) = @_;
    if (!defined $s) {
        $s = "";
    }
    $s =~ s/&/&amp;/g;
    $s =~ s/</&lt;/g;
    $s =~ s/>/&gt;/g;
    $s =~ s/"/&quot;/g;
    return $s;
}
