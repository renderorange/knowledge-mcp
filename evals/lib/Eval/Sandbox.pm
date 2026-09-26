package Eval::Sandbox;

use strict;
use warnings;
use Carp ();
use Cwd ();
use File::Basename ();
use File::Find ();
use File::Path ();
use File::Spec ();
use File::Temp ();
use JSON::PP ();
use Try::Tiny ();

my $_MODULE_DIR = File::Basename::dirname(File::Spec->rel2abs(__FILE__));
my $_DEFAULT_FIXTURES = Cwd::abs_path(File::Spec->catdir($_MODULE_DIR, "..", "..", "fixtures"));

sub build {
    my ($class, $sc, $opts) = @_;
    if (ref($sc) ne "HASH") {
        Carp::confess("build requires a scenario hashref");
    }
    if (ref($opts) ne "HASH") {
        $opts = {};
    }
    my $model = defined $opts->{model} && "$opts->{model}" =~ /\S/ ? "$opts->{model}" : "mimo/mimo-v2.6-pro";
    my ($provider_key) = split m{/}, $model, 2;
    my $root = File::Temp::tempdir("eval-XXXXXX", TMPDIR => 1, CLEANUP => 0);
    my %sb = (
        root          => $root,
        home          => "$root/home",
        project       => "$root/project",
        store         => "$root/store",
        transcript    => "$root/transcript.jsonl",
        tool_log      => "$root/tool.jsonl",
        model         => $model,
        provider_key  => $provider_key,
    );
    File::Path::make_path($sb{home} . "/.config/opencode/plugins", $sb{project}, $sb{store});

    for my $f (@{ $sc->{setup}{files} || [] }) {
        my $p = _contained_file($sb{project}, $f->{path});
        File::Path::make_path(_dirname($p));
        open my $fh, ">:encoding(UTF-8)", $p or Carp::confess("write $p: $!");
        print {$fh} defined $f->{content} ? $f->{content} : "";
        close $fh;
    }

    my $fixtures_dir = $opts->{fixtures_dir} || $_DEFAULT_FIXTURES;
    my $fixture = Cwd::abs_path("$fixtures_dir/AGENTS.md");
    if (!defined $fixture) {
        Carp::confess("AGENTS.md fixture not found in $fixtures_dir");
    }
    _copy($fixture, "$sb{project}/AGENTS.md");

    my $bin = $opts->{bin} || "knowledge-mcp";
    if ($bin =~ m{/} && -e $bin) {
        my $abs = Cwd::abs_path($bin);
        if (defined $abs) {
            $bin = $abs;
        }
    }
    my $cfg = {
        mcp => {
            "knowledge-mcp" => {
                command => [$bin, "--project", $sb{project}, "--index", "$root/index"],
                type    => "local",
            },
        },
    };
    my $host_cfg_path = $opts->{host_config} || "$ENV{HOME}/.config/opencode/opencode.jsonc";
    my $provider = _lift_provider($host_cfg_path, $provider_key);
    my $provider_lifted = 0;
    if (defined $provider) {
        $cfg->{provider} = $provider;
        $provider_lifted = 1;
    }
    open my $cf, ">", "$sb{home}/.config/opencode/opencode.jsonc" or Carp::confess("write config: $!");
    print {$cf} JSON::PP::encode_json($cfg);
    close $cf;

    _write_tool_log_plugin("$sb{home}/.config/opencode/plugins/eval-tool-log.ts", $sb{tool_log});

    if ($opts->{make_git}) {
        system("git", "init", "-q", $sb{project}) == 0 or Carp::confess("git init failed");
    }

    $sb{manifest_before} = _manifest($sb{project});
    $sb{timeout_sec} = $sc->{timeout_sec} || 120;
    $sb{provider_lifted} = $provider_lifted;
    return bless \%sb, $class;
}

sub teardown {
    my ($self) = @_;
    if (-d $self->{root}) {
        File::Path::remove_tree($self->{root});
    }
    return;
}

sub write_file {
    my ($self, $args) = @_;
    if (ref($args) ne "HASH") {
        Carp::confess("write_file requires a hashref arg");
    }
    my $p = _contained_file($self->{project}, $args->{path});
    File::Path::make_path(_dirname($p));
    open my $fh, ">:encoding(UTF-8)", $p or Carp::confess("write $p: $!");
    print {$fh} defined $args->{content} ? $args->{content} : "";
    close $fh;
    return;
}

# _contained_file resolves $rel against the sandbox root and confesses if the
# result would land outside it (traversal or absolute fixture paths).
sub _contained_file {
    my ($root, $rel) = @_;
    if (!defined $rel || $rel eq "") {
        Carp::confess("fixture path is required");
    }
    if (File::Spec->file_name_is_absolute($rel)) {
        Carp::confess("fixture path escapes sandbox root (absolute): $rel");
    }
    my @parts;
    for my $part (File::Spec->splitdir($rel)) {
        if ($part eq "" || $part eq ".") {
            next;
        }
        if ($part eq "..") {
            if (!@parts) {
                Carp::confess("fixture path escapes sandbox root: $rel");
            }
            pop @parts;
            next;
        }
        push @parts, $part;
    }
    if (!@parts) {
        Carp::confess("fixture path escapes sandbox root: $rel");
    }
    my $p = File::Spec->canonpath(File::Spec->catfile($root, @parts));
    my $prefix = File::Spec->canonpath($root);
    if (index($p, "$prefix/") != 0) {
        Carp::confess("fixture path escapes sandbox root: $rel");
    }
    return $p;
}

sub _manifest {
    my ($root) = @_;
    my %m;
    File::Find::find(sub {
        if (-d $_) {
            return;
        }
        my $rel = $File::Find::name;
        $rel =~ s{^\Q$root\E/}{};
        $m{$rel} = 1;
    }, $root);
    return \%m;
}

sub _dirname {
    my ($p) = @_;
    $p =~ s{/[^/]+$}{};
    return $p;
}

sub _copy {
    my ($src, $dst) = @_;
    open my $in, "<", $src or Carp::confess("read $src: $!");
    open my $out, ">", $dst or Carp::confess("write $dst: $!");
    local $/;
    print {$out} scalar <$in>;
    close $in;
    close $out;
    return;
}

# _lift_provider copies the provider block for the requested key out of the
# host opencode config so sandbox runs auth against the developer's real
# provider without hardcoding credentials in the repository.
sub _lift_provider {
    my ($path, $provider_key) = @_;
    if (!defined $provider_key || $provider_key !~ /\S/) {
        return undef;
    }
    if (!-r $path) {
        return undef;
    }
    my $raw = "";
    if (open my $fh, "<", $path) {
        local $/;
        $raw = <$fh> // "";
        close $fh;
    }
    else {
        return undef;
    }
    my $doc;
    Try::Tiny::try {
        $doc = JSON::PP::decode_json($raw);
    }
    Try::Tiny::catch {
        $doc = undef;
    };
    if (ref($doc) ne "HASH") {
        return undef;
    }
    if (ref($doc->{provider}) ne "HASH") {
        return undef;
    }
    if (ref($doc->{provider}{$provider_key}) ne "HASH") {
        return undef;
    }
    return { $provider_key => $doc->{provider}{$provider_key} };
}

# _write_tool_log_plugin generates the opencode tool.execute.after plugin that
# appends tool-call JSONL rows to the sandbox tool_log (same plugin-event
# pattern as the repo hook/ package; local plugins in
# $HOME/.config/opencode/plugins/ auto-load at startup).
sub _write_tool_log_plugin {
    my ($path, $log_path) = @_;
    my $safe = $log_path;
    $safe =~ s{\\}{\\\\}g;
    $safe =~ s{"}{\\"}g;
    my $src = '// knowledge-mcp eval runner tool log (generated by Eval::Sandbox)' . "\n"
        . 'import { appendFileSync } from "node:fs";' . "\n"
        . "\n"
        . 'const LOG = "' . $safe . '";' . "\n"
        . "\n"
        . 'export const EvalToolLog = async () => ({' . "\n"
        . '  "tool.execute.after": async (input, output) => {' . "\n"
        . '    try {' . "\n"
        . '      const name = input?.tool ?? "unknown";' . "\n"
        . '      const args = output?.args ?? input?.args ?? {};' . "\n"
        . '      let argv = [];' . "\n"
        . '      if (typeof args.command === "string") {' . "\n"
        . '        argv = args.command.trim().split(/\\s+/);' . "\n"
        . '      } else {' . "\n"
        . '        argv = Object.values(args).map((v) => String(v));' . "\n"
        . '      }' . "\n"
        . '      appendFileSync(LOG, JSON.stringify({ tool: name, argv: argv }) + "\\n");' . "\n"
        . '    } catch (err) {' . "\n"
        . '      // a hook must never break the client tool it augments' . "\n"
        . '    }' . "\n"
        . '  },' . "\n"
        . '});' . "\n";
    open my $fh, ">", $path or Carp::confess("write $path: $!");
    print {$fh} $src;
    close $fh;
    return;
}

1;
