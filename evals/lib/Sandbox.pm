package Eval::Sandbox;

use strict;
use warnings;
use Carp qw(confess);
use File::Path qw(make_path remove_tree);
use File::Temp qw(tempdir);
use JSON::PP qw(encode_json);
use Cwd qw(abs_path);
use FindBin;

sub build {
    my ($class, $sc, $opts) = @_;
    $opts ||= {};
    my $root = tempdir('eval-XXXXXX', TMPDIR => 1, CLEANUP => 0);
    my %sb = (
        root     => $root,
        home     => "$root/home",
        project  => "$root/project",
        store    => "$root/store",
        transcript => "$root/transcript.jsonl",
        tool_log   => "$root/tool.jsonl",
    );
    make_path($sb{home} . '/.config/opencode', $sb{project}, $sb{store});

    for my $f (@{ $sc->{setup}{files} || [] }) {
        my $p = "$sb{project}/$f->{path}";
        make_path(_dirname($p));
        open my $fh, '>', $p or confess "write $p: $!";
        print {$fh} $f->{content} // '';
        close $fh;
    }

    my $fixtures_dir = $opts->{fixtures_dir} || "$FindBin::Bin/../fixtures";
    my $fixture = abs_path("$fixtures_dir/AGENTS.md");
    _copy($fixture, "$sb{project}/AGENTS.md");

    my $bin = $opts->{bin} || 'knowledge-mcp';
    my $cfg = {
        mcp => {
            'knowledge-mcp' => {
                command => [$bin, '--project', $sb{project}, '--index', "$root/index"],
                type    => 'local',
            },
        },
    };
    open my $cf, '>', "$sb{home}/.config/opencode/opencode.jsonc" or confess $!;
    print {$cf} encode_json($cfg);
    close $cf;

    if ($opts->{make_git}) {
        system('git', 'init', '-q', $sb{project}) == 0 or confess "git init failed";
    }

    $sb{manifest_before} = _manifest($sb{project});
    $sb{timeout_sec} = $sc->{timeout_sec} || 120;
    return bless \%sb, $class;
}

sub teardown {
    my ($self) = @_;
    remove_tree($self->{root}) if -d $self->{root};
    return;
}

sub _manifest {
    my ($root) = @_;
    my %m;
    require File::Find;
    File::Find::find(sub {
        return if -d $_;
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
    open my $in, '<', $src or confess "read $src: $!";
    open my $out, '>', $dst or confess "write $dst: $!";
    local $/;
    print {$out} scalar <$in>;
    close $in;
    close $out;
}

1;
