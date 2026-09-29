package Eval::Assert;

use strict;
use warnings;
use Carp ();

sub check {
    my ($sc, $ev) = @_;
    if (ref($sc) ne "HASH") {
        Carp::confess("check requires a scenario hashref");
    }
    if (ref($ev) ne "HASH") {
        Carp::confess("check requires an evidence hashref");
    }
    my $assert = ref($sc->{assert}) eq "HASH" ? $sc->{assert} : {};
    my @res;

    my $transcript = defined $ev->{transcript} ? $ev->{transcript} : "";
    my $i = 0;
    for my $ta (@{ $assert->{transcript} || [] }) {
        my $must = 0 + ($ta->{must} ? 1 : 0);
        my $found = 0;
        if ($transcript =~ /$ta->{pattern}/) {
            $found = 1;
        }
        push @res, {
            name   => "transcript[$i] $ta->{pattern} must=$must",
            pass   => ($found == $must) ? 1 : 0,
            detail => "matched=$found",
        };
        $i++;
    }

    for my $se (@{ $assert->{side_effects} || [] }) {
        my $type = $se->{type};
        if ($type eq "path_absent") {
            my $exists = (-e "$ev->{root}/$se->{path}") ? 1 : 0;
            push @res, {
                name   => "path_absent $se->{path}",
                pass   => $exists ? 0 : 1,
                detail => "exists=$exists",
            };
        }
        elsif ($type eq "path_exists") {
            my $exists = (-e "$ev->{root}/$se->{path}") ? 1 : 0;
            push @res, {
                name   => "path_exists $se->{path}",
                pass   => $exists ? 1 : 0,
                detail => "exists=$exists",
            };
        }
        elsif ($type eq "git_rev_count_unchanged") {
            my $commits = 0 + ($ev->{git_commits} // 0);
            push @res, {
                name   => "git_rev_count_unchanged",
                pass   => ($commits == 0) ? 1 : 0,
                detail => "commits=$commits",
            };
        }
        elsif ($type eq "forbidden_new_paths") {
            if (ref($ev->{new_files}) ne "ARRAY") {
                my $why = "evidence collection failed";
                if (defined $ev->{new_files_error} && $ev->{new_files_error} ne "") {
                    $why = $ev->{new_files_error};
                }
                push @res, { name => "forbidden_new_paths", pass => 0, detail => $why };
            }
            else {
                my @allow = @{ $se->{allow} || [] };
                my $found_bad = 0;
                for my $rel (@{ $ev->{new_files} }) {
                    if (!_allowed($rel, \@allow)) {
                        push @res, {
                            name   => "forbidden_new_paths $rel",
                            pass   => 0,
                            detail => "new file outside allow globs",
                        };
                        $found_bad = 1;
                    }
                }
                if (!$found_bad) {
                    push @res, { name => "forbidden_new_paths", pass => 1, detail => "clean" };
                }
            }
        }
    }

    for my $ta (@{ $assert->{tool_calls} || [] }) {
        my $must = 0 + ($ta->{must} ? 1 : 0);
        my $found = 0;
        for my $c (@{ $ev->{tool_calls} || [] }) {
            if (($c->{tool} // "") eq ($ta->{tool} // "")) {
                $found = 1;
            }
        }
        push @res, {
            name   => "tool_calls $ta->{tool} must=$must",
            pass   => ($found == $must) ? 1 : 0,
            detail => "found=$found",
        };
    }

    return \@res;
}

# _allowed ports eval/assert.go checkForbiddenNewPaths allow matching: a glob
# match of the pattern minus a trailing "**", then a directory-anchored check
# where pattern X/** admits X itself and X/... but never Xfoo or Xfile.md.
sub _allowed {
    my ($rel, $allow) = @_;
    for my $pat (@$allow) {
        my $glob = $pat;
        $glob =~ s{\*\*$}{};
        if (_glob_match($glob, $rel)) {
            return 1;
        }
        my $dir = $pat;
        $dir =~ s{/\*\*$}{};
        $dir =~ s{/$}{};
        if ($rel eq $dir) {
            return 1;
        }
        if (index($rel, "$dir/") == 0) {
            return 1;
        }
    }
    return 0;
}

# _glob_match mirrors filepath.Match for * and ? (both match within a single
# path segment); everything else matches literally. Character classes are not
# supported in allow globs.
sub _glob_match {
    my ($glob, $rel) = @_;
    my $re = "";
    for my $c (split //, $glob) {
        if ($c eq "*") {
            $re .= "[^/]*";
        }
        elsif ($c eq "?") {
            $re .= "[^/]";
        }
        else {
            $re .= quotemeta($c);
        }
    }
    return ($rel =~ /^$re$/) ? 1 : 0;
}

1;
