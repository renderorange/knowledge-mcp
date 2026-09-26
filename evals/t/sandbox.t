use strict;
use warnings;
use Test::More;
use FindBin;
use lib "$FindBin::Bin/../lib";
use File::Spec ();
use File::Temp ();
use JSON::PP ();
use Try::Tiny ();
use Eval::Sandbox;

my $sc = {
    id          => "pilot",
    prompt      => "x",
    setup       => { files => [ { path => "src/app.js", content => "console.log(1);\n" } ] },
    timeout_sec => 30,
};

my $sb = Eval::Sandbox->build($sc, { make_git => 1 });
ok(-d $sb->{home}, "home exists");
ok(-d $sb->{project}, "project exists");
ok(-f "$sb->{project}/src/app.js", "fixture written");
ok(-f "$sb->{project}/AGENTS.md", "AGENTS.md fixture copied");
ok(-f "$sb->{home}/.config/opencode/opencode.jsonc", "opencode config generated");
ok(-d "$sb->{project}/.git", "git initialized");
ok(exists $sb->{manifest_before}{"src/app.js"}, "manifest captured");

my $cfg = "";
if (open my $fh, "<", "$sb->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $cfg = <$fh> // "";
    close $fh;
}
like($cfg, qr/knowledge-mcp/, "config wires knowledge-mcp");
like($cfg, qr/\Q$sb->{project}\E/, "config points at sandbox project");
unlike($cfg, qr/agents_global/, "config never references real store");

my $plugin = "$sb->{home}/.config/opencode/plugins/eval-tool-log.ts";
ok(-f $plugin, "tool-log plugin generated for tool-call capture");
my $plugin_src = "";
if (open my $pf, "<", $plugin) {
    local $/;
    $plugin_src = <$pf> // "";
    close $pf;
}
like($plugin_src, qr/tool\.execute\.after/, "plugin hooks tool.execute.after");
like($plugin_src, qr/\Q$sb->{tool_log}\E/, "plugin appends to the sandbox tool_log");

$sb->teardown();
ok(!-d $sb->{root}, "teardown removes sandbox");

my $tmp = File::Temp::tempdir(CLEANUP => 1);

my $host_cfg = "$tmp/host.json";
my $host_raw = '{"provider":{"mimo":{"npm":"@ai-sdk/openai-compatible","name":"MiMo","options":'
    . '{"baseURL":"https://example.invalid/v1","apiKey":"test-key"},"models":'
    . '{"mimo-v2.6-pro":{"name":"mimo-v2.6-pro"}}}}}';
if (open my $hf, ">", $host_cfg) {
    print {$hf} $host_raw;
    close $hf;
}
my $sb_prov = Eval::Sandbox->build($sc, {
    make_git       => 0,
    host_config    => $host_cfg,
    fixtures_dir   => "$FindBin::Bin/../fixtures",
});
ok($sb_prov->{provider_lifted}, "provider lifted from host config");
my $prov_raw = "";
if (open my $cf, "<", "$sb_prov->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $prov_raw = <$cf> // "";
    close $cf;
}
my $prov_doc = JSON::PP::decode_json($prov_raw);
is($prov_doc->{provider}{mimo}{npm}, "\@ai-sdk/openai-compatible", "generated config carries the mimo provider block");
is($prov_doc->{provider}{mimo}{options}{baseURL}, "https://example.invalid/v1", "provider options preserved");
$sb_prov->teardown();

my $sb_noprov = Eval::Sandbox->build($sc, {
    make_git       => 0,
    host_config    => "$tmp/absent.json",
    fixtures_dir   => "$FindBin::Bin/../fixtures",
});
ok(!$sb_noprov->{provider_lifted}, "provider_lifted is false without a readable host config");
my $noprov_raw = "";
if (open my $nf, "<", "$sb_noprov->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $noprov_raw = <$nf> // "";
    close $nf;
}
my $noprov_doc = JSON::PP::decode_json($noprov_raw);
ok(!exists $noprov_doc->{provider}, "no provider block invented when host config is absent");
$sb_noprov->teardown();

my $sb_relbin = Eval::Sandbox->build($sc, {
    make_git       => 0,
    bin            => "./knowledge-mcp",
    host_config    => "$tmp/absent.json",
    fixtures_dir   => "$FindBin::Bin/../fixtures",
});
my $relbin_raw = "";
if (open my $rf, "<", "$sb_relbin->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $relbin_raw = <$rf> // "";
    close $rf;
}
my $relbin_doc = JSON::PP::decode_json($relbin_raw);
my $relbin_cmd = $relbin_doc->{mcp}{"knowledge-mcp"}{command}[0];
ok($relbin_cmd =~ m{^/}, "relative KNM_BIN is absolutized in the mcp command");
ok(-e $relbin_cmd, "absolutized mcp command points at a real binary");
$sb_relbin->teardown();

my $trav_died = 0;
my $trav_err = "";
Try::Tiny::try {
    Eval::Sandbox->build({
        id          => "trav",
        prompt      => "x",
        timeout_sec => 5,
        setup       => { files => [ { path => "../escaped.txt", content => "x" } ] },
    }, {});
}
Try::Tiny::catch {
    $trav_died = 1;
    $trav_err = $_;
};
ok($trav_died, "build confesses on fixture path traversal");
like($trav_err, qr/escapes/, "traversal error names the escape");

my $abs_died = 0;
Try::Tiny::try {
    Eval::Sandbox->build({
        id          => "abs",
        prompt      => "x",
        timeout_sec => 5,
        setup       => { files => [ { path => "/tmp/eval-absolute-escape.txt", content => "x" } ] },
    }, {});
}
Try::Tiny::catch {
    $abs_died = 1;
};
ok($abs_died, "build confesses on absolute fixture path");
ok(!-e "/tmp/eval-absolute-escape.txt", "absolute path never written");

my $sb_dotdot = Eval::Sandbox->build({
    id          => "dotdot",
    prompt      => "x",
    timeout_sec => 5,
    setup       => { files => [ { path => "a/../b.txt", content => "inside\n" } ] },
}, {});
ok(-f "$sb_dotdot->{project}/b.txt", "a/../b.txt resolves inside the sandbox");
ok(!-e "$sb_dotdot->{root}/b.txt", "a/../b.txt does not land outside the project");
$sb_dotdot->teardown();

my $write_died = 0;
my $sb_write = Eval::Sandbox->build($sc, { make_git => 0, fixtures_dir => "$FindBin::Bin/../fixtures" });
Try::Tiny::try {
    $sb_write->write_file({ path => "../../evil.txt", content => "x" });
}
Try::Tiny::catch {
    $write_died = 1;
};
ok($write_died, "write_file confesses on path traversal");
ok(!-e "$sb_write->{root}/../evil.txt", "write_file never escapes the sandbox root");
$sb_write->write_file({ path => "tmp/ok.txt", content => "y" });
ok(-f "$sb_write->{project}/tmp/ok.txt", "write_file writes contained paths");
$sb_write->teardown();

my $lib_dir = File::Spec->catdir($FindBin::Bin, "..", "lib");
my $probe_src = 'use strict;
use warnings;
use lib "' . $lib_dir . '";
use Eval::Sandbox;
my $sb = Eval::Sandbox->build({ id => "probe", prompt => "x", timeout_sec => 5 }, {});
my $ok = -f "$sb->{project}/AGENTS.md" ? 1 : 0;
$sb->teardown();
exit($ok ? 0 : 2);
';
my $probe = "$tmp/probe.pl";
if (open my $pr, ">", $probe) {
    print {$pr} $probe_src;
    close $pr;
}
my $probe_rc = system($^X, $probe);
is($probe_rc >> 8, 0, "fixtures_dir default is module-relative (works from a foreign script)");

my $host_cfg2 = "$tmp/host2.json";
my $host_raw2 = '{"provider":{"anthropic":{"npm":"@ai-sdk/anthropic","name":"Anthropic","options":'
    . '{"baseURL":"https://example.invalid/v2","apiKey":"test-key"},"models":'
    . '{"claude-sonnet-4":{"name":"claude-sonnet-4"}}},'
    . '"mimo":{"npm":"@ai-sdk/openai-compatible","name":"MiMo","options":'
    . '{"baseURL":"https://example.invalid/v1","apiKey":"test-key"},'
    . '"models":{"mimo-v2.6-pro":{"name":"mimo-v2.6-pro"}}}}}';
if (open my $h2, ">", $host_cfg2) {
    print {$h2} $host_raw2;
    close $h2;
}

my $sb_anth = Eval::Sandbox->build($sc, {
    make_git     => 0,
    model        => "anthropic/claude-sonnet-4",
    host_config  => $host_cfg2,
    fixtures_dir => "$FindBin::Bin/../fixtures",
});
is($sb_anth->{model}, "anthropic/claude-sonnet-4", "model option is stored on the sandbox");
is($sb_anth->{provider_key}, "anthropic", "provider key derived from the model prefix");
ok($sb_anth->{provider_lifted}, "provider lifted for a non-mimo model");
my $anth_raw = "";
if (open my $af, "<", "$sb_anth->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $anth_raw = <$af> // "";
    close $af;
}
my $anth_doc = JSON::PP::decode_json($anth_raw);
is($anth_doc->{provider}{anthropic}{npm}, "\@ai-sdk/anthropic", "generated config carries the anthropic provider block");
ok(!exists $anth_doc->{provider}{mimo}, "unrelated provider block is not lifted");
$sb_anth->teardown();

my $sb_default = Eval::Sandbox->build($sc, {
    make_git     => 0,
    host_config  => $host_cfg,
    fixtures_dir => "$FindBin::Bin/../fixtures",
});
is($sb_default->{model}, "mimo/mimo-v2.6-pro", "missing model option falls back to the default pin");
is($sb_default->{provider_key}, "mimo", "default model yields the mimo provider key");
ok($sb_default->{provider_lifted}, "default model still lifts the mimo provider block");
$sb_default->teardown();

my $sb_missing = Eval::Sandbox->build($sc, {
    make_git     => 0,
    model        => "openai/gpt-x",
    host_config  => $host_cfg,
    fixtures_dir => "$FindBin::Bin/../fixtures",
});
ok(!$sb_missing->{provider_lifted}, "provider_lifted is false when the requested provider block is absent");
my $miss_raw = "";
if (open my $mf, "<", "$sb_missing->{home}/.config/opencode/opencode.jsonc") {
    local $/;
    $miss_raw = <$mf> // "";
    close $mf;
}
my $miss_doc = JSON::PP::decode_json($miss_raw);
ok(!exists $miss_doc->{provider}, "no provider block invented when the requested provider is absent");
$sb_missing->teardown();

done_testing
