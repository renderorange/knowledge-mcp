use strict;
use warnings;
use Test::More;
use FindBin;
use File::Copy ();
use File::Path ();
use File::Spec ();
use File::Temp ();

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
_copy_rel({ rel => "lib/Eval/Judge.pm" });
_copy_rel({ rel => "fixtures/AGENTS.md" });

my $probe_yaml = 'id: tool-probe
name: MCP tool name normalization probe
tier: both
tags: [probe]
prompt: |
  probe
assert:
  tool_calls:
    - tool: query_knowledge
      must: true
    - tool: bash
      must: true
    - tool: knowledge-mcp_query_knowledge
      must: false
timeout_sec: 30
';
_write_file({
    path    => File::Spec->catfile($dst_evals, "scenarios", "tool-probe.yaml"),
    content => $probe_yaml,
});

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
my $home = defined $ENV{HOME} ? $ENV{HOME} : "";
my $log = "";
my $plugin = "$home/.config/opencode/plugins/eval-tool-log.ts";
if (open my $ph, "<", $plugin) {
    local $/;
    my $src = <$ph> // "";
    close $ph;
    if ($src =~ /const LOG = "([^"]+)"/) {
        $log = $1;
    }
}
if (defined $ENV{FAKE_MODE} && $ENV{FAKE_MODE} eq "nostrip") {
    my $cfg = defined $ENV{OPENCODE_CONFIG} ? $ENV{OPENCODE_CONFIG} : "";
    if ($cfg ne "") {
        unlink $cfg;
    }
}
if ($log ne "") {
    if (open my $lh, ">>", $log) {
        print {$lh} JSON::PP::encode_json({ tool => "knowledge-mcp_query_knowledge", argv => ["query_knowledge"] }) . "\n";
        print {$lh} JSON::PP::encode_json({ tool => "bash", argv => ["git", "status"] }) . "\n";
        close $lh;
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
    local $ENV{FAKE_MODE} = $args->{mode};
    my $out = "";
    my $exit = 0;
    if (open my $fh, "-|", $^X, $run_pl, "--scenario", "tool-probe") {
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

my ($exit_a, $out_a) = _run_probe({ mode => "strip" });
is($exit_a, 0, "readable config run exits 0");
like($out_a, qr/\[PASS\] tool-probe/, "readable config run passes the scenario");
like($out_a, qr/tool_calls query_knowledge must=1: ok/, "prefixed MCP tool yields the short evidence tool name");
like($out_a, qr/tool_calls bash must=1: ok/, "unprefixed tool name is unchanged");
like($out_a, qr/tool_calls knowledge-mcp_query_knowledge must=0: ok/, "prefixed name is gone from evidence after normalization");

my ($exit_b, $out_b) = _run_probe({ mode => "nostrip" });
is($exit_b, 1, "unreadable config run exits non-zero");
like($out_b, qr/tool_calls query_knowledge must=1: FAIL \(found=0\)/, "unreadable config leaves the prefixed name unstripped");
like($out_b, qr/tool_calls bash must=1: ok/, "unprefixed tool name is unchanged without config");
like(
    $out_b,
    qr/tool_calls knowledge-mcp_query_knowledge must=0: FAIL \(found=1\)/,
    "names pass through unstripped when config cannot be read",
);

done_testing
