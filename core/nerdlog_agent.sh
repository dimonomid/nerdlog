# NOTE: we intentionally don't rely on shebang here, and expect this script to
# be invoked as "bash nerdlog_agent.sh" explicitly, since it's likely the most
# portable way (short of using /bin/sh, but that would be a bigger effort since
# this script does rely on bash).

# NOTE: ABANDON ALL HOPE.
#
# This script logic is really convoluted and hard to understand, and begs for a
# major rewrite.

trap 'echo "exit_code:$?"' EXIT

# Arguments:
#
# --from, --to: time in the format "2006-01-02-15:04".

# Those numbers are supposed to go up as the query progresses; the Go app
# will then be able to tell which node is the slowest and show info for it.
STAGE_INDEX_FULL=1
STAGE_INDEX_APPEND=2
STAGE_QUERYING=3
STAGE_DONE=4

SPECIAL_FILENAME_AUTO="auto"
SPECIAL_FILENAME_JOURNALCTL="journalctl"

# The output looks like this:
# 2025-04-27T21:31:11.670468+00:00 myhot systemd[1]: Something happened.
JOURNALCTL_FORMAT_FLAG="--output=short-iso-precise"

indexfile=/tmp/nerdlog_agent_index

logfile_prev="${SPECIAL_FILENAME_AUTO}"
logfile_last="${SPECIAL_FILENAME_AUTO}"

positional_args=()

max_num_lines=100

awktime_month='monthByName[substr($0, 1, 3)]'
awktime_year='yearByMonth[month]'
awktime_day='(substr($0, 5, 1) == " ") ? "0" substr($0, 6, 1) : substr($0, 5, 2)'
awktime_hhmm='substr($0, 8, 5)'
awktime_minute_key='substr($0, 1, 12)'
# TODO: double check that if any of these is provided manually in a flag,
# then all of them are provided manually.

function find_gawk_binary() { # {{{
  gawk_path="$(which gawk)"
  if [[ $? == 0 ]]; then
    if [ -x "$gawk_path" ]; then
      awk_version_str="$($gawk_path --version)"
      if [[ $? == 0 ]]; then
        if echo "$awk_version_str" | grep -q 'GNU Awk'; then
          # gawk works fine
          echo "$gawk_path"
          exit 0
        fi
      fi
    fi
  fi

  awk_path="$(which awk)"
  if [[ $? == 0 ]]; then
    if [ -x "$awk_path" ]; then
      awk_version_str="$($awk_path --version)"
      if [[ $? == 0 ]]; then
        if echo "$awk_version_str" | grep -q 'GNU Awk'; then
          # awk works fine
          echo "$awk_path"
          exit 0
        fi
      fi
    fi
  fi

  exit 1
} # }}}

function detect_timezone() { # {{{
  # Prefer TZ env var if available
  if [[ "$TZ" != "" ]]; then
    echo "$TZ"
    exit 0
  fi

  # Next check timedatectl if available
  host_timezone="$(timedatectl show --property=Timezone --value)"
  if [[ $? == 0 ]]; then
    echo "$host_timezone"
    exit 0
  fi

  # Resort to /etc/timezone
  if [ -r /etc/timezone ]; then
    host_timezone="$(cat /etc/timezone)"
    if [[ $? == 0 ]]; then
      echo "$host_timezone"
      exit 0
    fi
  fi

  # Then resort to browsing zoneinfo files manually
  zone_file="$(find /usr/share/zoneinfo  -type f -exec cmp -s /etc/localtime '{}' \; -print)"
  if [[ $? == 0 ]]; then
    host_timezone="$(echo "$zone_file" | sed -e 's|^/usr/share/zoneinfo/||' -e '/posix/d')"
    if [[ $? == 0 ]]; then
      echo "$host_timezone"
      exit 0
    fi
  fi

  exit 1
} # }}}

# function concat_cmds_array() {{{
#
# Concatenates the global `cmds` array into a single bash command, using " && ".
# Escapes things properly. The result can be passed to "eval" or "bash -c".
function concat_cmds_array() {
  local first=1
  for cmd in "${cmds[@]}"; do
    if [[ $first == 1 ]]; then
      first=0
    else
      echo -n " && "
    fi
    echo -n "${cmd//\'/\'\"\'\"\'}"
  done
} # }}}

while [[ $# -gt 0 ]]; do
  case $1 in
    -c|--index-file)
      indexfile="$2"
      shift # past argument
      shift # past value
      ;;
    --logfile-last)
      logfile_last="$2"
      shift # past argument
      shift # past value
      ;;
    --logfile-prev)
      logfile_prev="$2"
      shift # past argument
      shift # past value
      ;;
    -f|--from)
      from="$2"
      shift # past argument
      shift # past value
      ;;
    -t|--to)
      to="$2"
      shift # past argument
      shift # past value
      ;;
    -u|--lines-until)
      lines_until="$2"
      shift # past argument
      shift # past value
      ;;

    # The 3 arguments below:
    # --timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest
    # are needed specifically for pagination in journalctl.
    #
    # It's all very ugly, but works correctly, and so far I'm not able to come
    # up with better alternatives (see below why --cursor etc isn't helpful for us).
    #
    # Let me explain what they mean exactly. Let's consider that we have the following
    # logs, some of which we already have loaded, and now we need to get the next page:
    #
    #    ........
    #    2025-03-10T11:49:44.123456+00:00 myhost myapp[123]: NEXT PAGE message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: NEXT PAGE message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.988548+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.988548+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.999000+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.999000+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:45.002143+00:00 myhost myapp[123]: LOADED message
    #
    # So we already have two messages at the "2025-03-10T11:49:44.838785"
    # timestamp, and the next page should start from the remaining one message
    # on the same timestamp.
    #
    # So first, even though we technically can pass the --until '2025-03-10 11:49:44.838785
    # argument to journalctl, which takes it without errors, it doesn't work
    # reliably: apparently the time indexing journalctl is doing does not have
    # microsecond precision, and it will actually stop EARLIER than the given
    # timestamp, missing arbitrary number of messages.
    #
    # To make sure that we do get all the messages we need, we have to request
    # a bit more, and round the --until timestamp to the next whole second:
    # '2025-03-10 11:49:45'. This is precisely what needs to be passed as the
    # --timestamp-until-seconds flag.
    #
    # And having that, we also need to know how many latest messages to filter
    # out, because we already have them. There are a few ways of doing it, but
    # currently implemented as follows:
    #
    # 1) --timestamp-until-precise is the exact timestamp of the very latest
    #    message we have, formatted the way journalctl formats it, i.e.
    #    '2025-03-10T11:49:44.838785'
    # 2) --skip-n-latest is how many messages we already have on this timestamp,
    #    i.e. '2' in this case.
    #
    # Having that, the agent script knows everything it needs to know, and the
    # logic is as follows (btw don't forget that we call journalctl with the
    # --reverse, so we first get the latest messages, which we need to skip):
    #
    # - Check if the current timestamp from journalctl string is
    #   lexicographically larger than the given --timestamp-until-precise. If
    #   so, just skip it: we're only receiving this line because our
    #   --timestamp-until-seconds was rounded up to the whole second
    # - Check if the current timestamp from journalctl string is exactly the
    #   same as the given --timestamp-until-precise. If so, skip up to the
    #   --skip-n-latest of such messages.
    # - Otherwise, we're good to include this message in the output.
    #
    # Now, why the journalctl built-in pagination mechanism (the --cursor and
    # related flags) doesn't work. Two primary reasons:
    #
    # - Journalctl only allows us to see the cursor of the *latest* message in
    #   the output;
    # - We apply our filters, and limit number of lines, *after* journalctl,
    #   using the awk script.
    #
    # So, there's no reliable way to say "print the latest N messages maching
    # this awk pattern, and show me the cursor of the first one, so that I can
    # later get next page".
    #
    # If there was a way to show the cursor for every single line that
    # journalctl outputs, then it might be possible, but then it would likely
    # make things even slower.
    #
    # So for now, we just have to hack around with the timestamps. Ugly, but
    # works, and covered with tests.

    --timestamp-until-seconds)
      timestamp_until_seconds="$2"
      shift # past argument
      shift # past value
      ;;
    --timestamp-until-precise)
      timestamp_until_precise="$2"
      if [[ "$skip_n_latest" == "" ]]; then
        skip_n_latest=1
      fi
      shift # past argument
      shift # past value
      ;;
    --skip-n-latest)
      skip_n_latest="$2"
      shift # past argument
      shift # past value
      ;;

    --refresh-index)
      refresh_index="1"
      shift # past argument
      ;;
    -l|--max-num-lines)
      max_num_lines="$2"
      shift # past argument
      shift # past value
      ;;

    --awktime-month)
      awktime_month="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-year)
      awktime_year="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-day)
      awktime_day="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-hhmm)
      awktime_hhmm="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-minute-key)
      awktime_minute_key="$2"
      shift # past argument
      shift # past value
      ;;

    -*|--*)
      echo "Unknown option $1" 1>&2
      exit 1
      ;;
    *)
      positional_args+=("$1") # save positional arg
      shift # past argument
      ;;
  esac
done

set -- "${positional_args[@]}" # restore positional parameters

if [[ $timestamp_until_precise != "" || $timestamp_until_seconds != "" || $skip_n_latest != "" ]]; then
  if [[ "$timestamp_until_precise" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --timestamp-until-precise is not set" 1>&2
    exit 1
  fi

  if [[ "$timestamp_until_seconds" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --timestamp-until-seconds is not set" 1>&2
    exit 1
  fi

  if [[ "$skip_n_latest" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --skip-n-latest is not set" 1>&2
    exit 1
  fi
fi

# Either use the provided current year and month (for tests), or get the actual ones.
if [[ "$CUR_YEAR" == "" ]]; then
  CUR_YEAR="$(date +'%Y')"
fi

if [[ "$CUR_MONTH" == "" ]]; then
  CUR_MONTH="$(date +'%m')"
fi

# TODO: instead of always detecting it, add support for the --awk-binary flag,
# and only autodetect if it wasn't provided. Also, gotta always do this during
# logstream_info command.
awk_binary="$(find_gawk_binary)"
if [[ $? != 0 ]]; then
  echo "error:gawk (GNU Awk) is a requirement, but not found on the system. Please install it, then retry" 1>&2
  exit 1
fi

# Use either a real journalctl, or a mocked one.
journalctl_binary="journalctl"
if [[ "${NERDLOG_JOURNALCTL_MOCK}" != "" ]]; then
  journalctl_binary="${NERDLOG_JOURNALCTL_MOCK}"
fi

os_kind=""
case "$(uname -s)" in
  Linux)
    os_kind="linux"
    ;;
  Darwin)
    os_kind="macos"
    ;;
  FreeBSD|OpenBSD|NetBSD|DragonFly)
    os_kind="bsd"
    ;;
  *)
    echo "error:unknown kernel name $(uname -s)" 2>&1
    exit 1
esac

# TODO: also check that gawk is recent enough; the -b option that we need
# was introduced in 4.0.0, released in 2011:
# https://lists.gnu.org/archive/html/info-gnu/2011-06/msg00013.html
# Since it's so old, not bothering to check the version for now.

if [[ "$logfile_last" == "${SPECIAL_FILENAME_AUTO}" ]]; then
  if [ -e /var/log/messages ]; then
    logfile_last=/var/log/messages
  elif [ -e /var/log/syslog ]; then
    logfile_last=/var/log/syslog
  elif command -v journalctl > /dev/null 2>&1; then
    logfile_last="${SPECIAL_FILENAME_JOURNALCTL}"
  else
    echo "error:failed to autodetect log file: neither /var/log/messages nor /var/log/syslog log files are present, and journalctl is not available either. Specify the log file manually" 1>&2
    exit 1
  fi
fi

if [[ "$logfile_prev" == "${SPECIAL_FILENAME_AUTO}" ]]; then
  if [[ "$logfile_last" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
    # For now just blindly append ".1" to the first logfile; if it doesn't actually
    # exist, we'll handle this case right below.
    logfile_prev="${logfile_last}.1"
  else
    # Set it to the same special value
    logfile_prev="${SPECIAL_FILENAME_JOURNALCTL}"
  fi
fi

# A simple hack to account for cases when /var/log/syslog.1 doesn't exist:
# create an empty file and pretend that it's an empty log file.
if [ ! -e "$logfile_prev" ] && [[ "$logfile_prev" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
  echo "debug:prev logfile $logfile_prev doesn't exist, using a dummy empty file /tmp/nerdlog-empty-file" 1>&2
  # TODO: instead of using the same file /tmp/nerdlog-empty-file , maybe
  # generate the name based on the index filename, to make the tests more
  # self-contained.
  logfile_prev="/tmp/nerdlog-empty-file"
  if [ ! -f "$logfile_prev" ] || [ -s "$logfile_prev" ]; then
    rm -f $logfile_prev || exit 1
    touch $logfile_prev || exit 1
  fi

  # For stable output in tests, also update the creation/modification time of
  # that file to be the same as the first log file. It's not portable though
  # (not gonna work on BSD), but it's non-essential functionality, so we just
  # ignore any errors here and do nothing then.
  ctime=$(stat -c %W $logfile_last 2>/dev/null)
  if [[ $? == 0 ]]; then
    touch -d "@$ctime" $logfile_prev
  fi
fi

command="$1"
if [[ "${command}" == "" ]]; then
  echo "error:command is required" 1>&2
  exit 1
fi

case "${command}" in
  query)
    shift
    # Will be handled below.
    ;;

  logstream_info)
    host_timezone="$(detect_timezone)"
    if [[ $? == 0 ]]; then
      echo "host_timezone:$host_timezone"
    else
      echo "warn:failed to detect host timezone"
    fi

    if [[ "${logfile_last}" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
      if [ ! -e ${logfile_last} ]; then
        echo "error:${logfile_last} does not exist" 1>&2
        exit 1
      fi

      if [ ! -r ${logfile_last} ]; then
        echo "error:${logfile_last} exists but is not readable, check your permissions" 1>&2
        exit 1
      fi

      if [ ! -e ${logfile_prev} ]; then
        echo "error:${logfile_prev} does not exist" 1>&2
        exit 1
      fi

      if [ ! -r ${logfile_prev} ]; then
        echo "error:${logfile_prev} exists but is not readable, check your permissions" 1>&2
        exit 1
      fi

      # Print a bunch of example log lines, so that the client can autodetect the
      # format.
      if [ -s ${logfile_last} ]; then
        last_line="$(tail -n 1 ${logfile_last})" || exit 1
        first_line="$(head -n 1 ${logfile_last})" || exit 1
        echo "example_log_line:$last_line"
        echo "example_log_line:$first_line"
      fi
      if [ -s ${logfile_prev} ]; then
        last_line="$(tail -n 1 ${logfile_prev})" || exit 1
        first_line="$(head -n 1 ${logfile_prev})" || exit 1
        echo "example_log_line:$last_line"
        echo "example_log_line:$first_line"
      fi
    else
      # We need to use journalctl, check if it's executable
      if ! command -v "$journalctl_binary" > /dev/null 2>&1; then
        echo "error:journalctl is not found" 1>&2
        exit 1
      fi

      # Check if the user has access to all system logs (as opposed to only its
      # own logs). Ideally we'd ask journalctl, but it doesn't seem to provide
      # a way to learn this easily, so for now just checking user id and groups
      # manually.
      if ! [[ "$(id -u)" == 0 || " $(id -Gn) " == *" adm "* || " $(id -Gn) " == *" systemd-journal "* ]]; then
        # User is not root, and is not in the adm or systemd-journal groups.
        # Print a warning so that the client script can show it on the UI somehow.
        echo "warn_journalctl_no_admin_access" 1>&2
      fi

      # And print one line for the timestamp format autodetection.
      last_line="$($journalctl_binary $JOURNALCTL_FORMAT_FLAG --quiet -n 1)" || exit 1
      echo "example_log_line:$last_line"
    fi

    exit 0
    ;;

  *)
    echo "error:invalid command ${command}" 1>&2
    exit 1
esac

# What follows is the handler for the "query" command.

# NOTE: we only show percentages with 5% increments, to save on traffic and
# other overhead. With all 24 my-nodes, having percentage being printed with
# 1% increments, it generates extra traffic of about 290KB per single query,
# wow. With 5% increments, the overhead is about 70 KB.
awk_func_print_percentage='
function printPercentage(numCur, numTotal) {
  curPercent = int(numCur/numTotal*20);
  if (curPercent != lastPercent) {
    print "p:p:" curPercent*5 >> "/dev/stderr"
    lastPercent = curPercent
  }
}
'

function run_awk_script_logfiles {
  awk_pattern=''
  if [[ "$user_pattern" != "" ]]; then
    awk_pattern="!($user_pattern) {numFilteredOut++; next}"
  fi

  # NOTE: this script MUST be executed with the "-b" awk key, which means that
  # awk will work in terms of bytes, not characters. We use length($0) there and
  # we rely on it being number of bytes.
  #
  # Also btw, percentage calculation slows the whole query by about 10%, which
  # isn't ideal. TODO: maybe instead of doing the division on every line, we can
  # only do the division when the percentage changes, so we calculate the next
  # point when it'd change, and going forward we just compare it with a simple
  # "<".
  awk_script='
  '$awk_func_print_percentage'

  BEGIN {
    bytenr=1; curline=0; maxlines='$max_num_lines'; lastPercent=0;
    numFilteredOut=0;
    prevMinKey="";
  }
  { bytenr += length($0)+1 }
  NR % 100 == 0 {
    printPercentage(bytenr, '$num_bytes_to_scan')
  }
  '$awk_pattern'
  {
    curMinKey = '"$awktime_minute_key"';

    # NOTE: this was a naive attempt to better handle the case when timestamps
    # have decreased: instead of incrementing the bucket of the decreased
    # timestamp, we ideally want to increment the bucket of the last
    # non-decreased timestamp.
    #
    # However, to make it work properly, the minute key needs to be formatted
    # so that a later timestamp is always lexocographically larger than an
    # earlier timestamp, and while it is possible to implement it this way,
    # it slows things down significantly, which seems unjustified just to
    # handle this corner case more gracefully.
    #
    # We might still implement it at some point and make it optional, but for
    # now, keeping things simple and just not caring about this corner case.
    #
    ## Account for decreased timestamps.
    ##
    ## NOTE: to make it produce the correct result in all cases, this check
    ## needs to be before the pattern check, but we intentionally avoid doing
    ## that because it slows things down by 5-10% when the pattern filters out
    ## most of the lines, which I think is not worth it to account for this
    ## corner case.
    #if (curMinKey < prevMinKey) {
      #curMinKey = prevMinKey;
    #} else {
      #prevMinKey = curMinKey;
    #}

    stats[curMinKey]++;

    '$lines_until_check'

    lastlines[curline] = $0;
    lastNRs[curline] = NR;
    curline++
    if (curline >= maxlines) {
      curline = 0;
    }

    next;
  }

  END {
    print "debug:Filtered out " numFilteredOut " from " NR " lines" > "/dev/stderr"

    print "logfile:'$logfile_prev':0";
    print "logfile:'$logfile_last':'$prevlog_lines'";

    for (x in stats) {
      print "s:" x "," stats[x]
    }

    for (i = 0; i < maxlines; i++) {
      ln = curline + i;
      if (ln >= maxlines) {
        ln -= maxlines;
      }

      if (!lastlines[ln]) {
        continue;
      }

      curNR = lastNRs[ln] + '$from_linenr_int' - 1;

      print "m:" curNR ":" lastlines[ln];
    }
  }
  '

  "$awk_binary" -b "$awk_script" "$@"
  if [[ "$?" != 0 ]]; then
    return 1
  fi
}

function run_awk_script_journalctl {
  awk_pattern_check=''
  if [[ "$user_pattern" != "" ]]; then
    awk_pattern_check="!($user_pattern) {numFilteredOut++; next}"
  fi

  awk_skip_n_latest_check=''
  if [[ "$timestamp_until_precise" != "" && "$skip_n_latest" != "" ]]; then
    awk_skip_n_latest_check='
    (needToSkip) {
      curtime = substr($0, 1, timestampUntilPreciseLen);

      # If the timestamp is larger than what we already have, just skip.
      if (curtime > timestampUntilPrecise) {
        next;
      }

      # If the timestamp is exactly the same as what we already have,
      # skip the skip_n_latest lines.
      if (curtime == timestampUntilPrecise) {
        numSameTimestamp++;
        if (numSameTimestamp <= '"$skip_n_latest"') {
          next;
        }

        # We have skipped enough lines, remember that
        print "debug:Skipped " NR-1 " latest lines" > "/dev/stderr"
        needToSkip = 0;
      }

      # If the timestamp is earlier than what we already have,
      # remember that we are done skipping, to avoid doing useless work.
      if (curtime < timestampUntilPrecise) {
        print "debug:Skipped " NR-1 " latest lines" > "/dev/stderr"
        needToSkip = 0;
      }
    }
    '
  fi

  early_exit_check=''
  if [[ "$stop_after_max_num_lines" != "" ]]; then
    early_exit_check='curline >= maxlines {
      print "debug:Exiting early after collecting " curline " lines" > "/dev/stderr"
      exit
    }'
  fi

  awk_script='
  '$awk_func_print_percentage'

  # Takes timestamp in the same format as we use for --from and --to and
  # store in the index ("2006-01-02-15:04"), and returns the corresponding unix
  # timestamp.
  function indexTimestrToTimestamp(timestr) {
    year = substr(timestr, 1, 4);
    month = substr(timestr, 6, 2);
    day = substr(timestr, 9, 2);
    hh = substr(timestr, 12, 2);
    mm = substr(timestr, 15, 2);

    return mktime(year " " month " " day " " hh " " mm " 00");
  }

  BEGIN {
    curline=0;
    lastline="";
    maxlines='$max_num_lines';
    numFilteredOut=0;
    lastPercent=-1;
    timestampUntilPrecise="'"$timestamp_until_precise"'";
    timestampUntilPreciseLen=length(timestampUntilPrecise);
    numSameTimestamp=0;
    needToSkip = timestampUntilPreciseLen > 0 ? 1 : 0;

    # Find out earliest and latest timestamp for percentage calculations.
    earliestTimestamp=0;
    latestTimestamp=0;

    if ("'$from'" != "") {
      earliestTimestamp = indexTimestrToTimestamp("'$from'");
    } else {
      # No "from" timestamp; technically it is possible to get it using
      # "journalctl --no-pager | head -n 1", but not bothering for now
      # because nerdlog always provides the --from.
      #
      # If it happens, the script will just not print any percentages
      # because timespanSeconds will be 0.
    }

    if ("'$to'" != "") {
      latestTimestamp = indexTimestrToTimestamp("'$to'");
    } else {
      # No "to" timestamp; just use the current time.
      latestTimestamp = systime();
    }

    timespanSeconds = 0;
    if (earliestTimestamp != 0 && latestTimestamp != 0) {
      timespanSeconds = latestTimestamp - earliestTimestamp;
    }
  }

  {
    # Unfortunately journalctl prints multiline messages without the leading
    # timestamp and other details: instead, they just add padding with spaces,
    # which breaks our parsing; so we manually replace this padding with the
    # details from the previous non-padded line.
    if (substr($0, 1, 1) == " ") {
      # Find out the number of leading spaces
      numLeadingSpace = length($0)
      if (NF > 0) {
        numLeadingSpace = index($0, $1) - 1;
      }

      if (length(lastline) < numLeadingSpace) {
        print "error:line has more leading whitespaces than the length of the previous line";
        exit 1;
      }

      # Replace these leading spaces with the same amount of characters from the previous line.
      $0 = substr(lastline, 1, numLeadingSpace) substr($0, numLeadingSpace + 1);
    }

    lastline = $0;
  }

  # Print percentage based on time. It is not as great as if it was
  # based on the number of bytes as we have it for the logfiles (because the
  # pace at which the percentage progresses will vary based on the intensivity
  # of the logs), but for journalctl we can hardly do any better.
  NR % 1000 == 0 {
    month = '"$awktime_month"';
    year = '"$awktime_year"';
    day = '"$awktime_day"';
    hhmm = '"$awktime_hhmm"';
    hh = substr(hhmm, 1, 2);
    mm = substr(hhmm, 4, 2);
    curTimestamp = mktime(year " " month " " day " " hh " " mm " 00");

    if (timespanSeconds > 0) {
      printPercentage(latestTimestamp-curTimestamp, timespanSeconds)
    } else {
      # We do not know the timespan, so just do not print any percentages.
    }
  }

  '$awk_pattern_check'
  '$awk_skip_n_latest_check'
  {
    stats['"$awktime_minute_key"']++;

    if (curline < maxlines) {
      lines[curline] = $0;
      curline++
    }
  }
  '$early_exit_check'

  END {
    print "debug:Filtered out " numFilteredOut " from " NR " lines" > "/dev/stderr"

    print "logfile:'$logfile_last':0";

    for (x in stats) {
      print "s:" x "," stats[x]
    }

    for (i = curline-1; i >= 0; i--) {
      print "m:0:" lines[i];
    }
  }
  '

  "$awk_binary" "$awk_script" "$@"
  if [[ "$?" != 0 ]]; then
    return 1
  fi
}

user_pattern=$1

if [[ "$logfile_last" == "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
  echo "p:stage:$STAGE_QUERYING:querying logs:Note that journalctl can be SLOW. Consider using log files." 1>&2

  # For both $from and $to, convert the format
  # "2006-01-02-15:04" -> "2006-01-02 15:04:00"
  journalctl_from=""
  if [[ "$from" != "" ]]; then
    journalctl_from="${from:0:10} ${from:11}:00"
  fi

  journalctl_to=""
  if [[ "$to" != "" ]]; then
    journalctl_to="${to:0:10} ${to:11}:00"
  fi

  stop_after_max_num_lines=""

  # Build journalctl command.
  #
  # --quiet is needed to suppress lines like "-- No entries --" or other
  # human-readable informative things; we only need logs since we parse them.
  #
  # --reverse is needed because it simplifies things and allows optimization in
  # some cases: in the awk script, we don't have to keep circular buffer for
  # all the lines and then print the last ones (like we do when reading log
  # files); and also when we're just getting the next page and not interested
  # in timeline histogram data for the full period, we just exit early after
  # accumulating $max_num_lines.
  cmd="$journalctl_binary $JOURNALCTL_FORMAT_FLAG --quiet --reverse"

  if [[ -n "$journalctl_from" ]]; then
    cmd="$cmd --since \"$journalctl_from\""
  fi

  if [[ -n "$timestamp_until_seconds" ]]; then
    cmd="$cmd --until \"$timestamp_until_seconds\""
    stop_after_max_num_lines="1"
    # NOTE: we'll also skip the $skip_n_latest messages with the latest timestamp.
  elif [[ -n "$journalctl_to" ]]; then
    cmd="$cmd --until \"$journalctl_to\""
  fi

  echo "debug:Command to filter logs by time range:" 1>&2
  echo "debug: $cmd" 1>&2

  eval "${cmd}" |                         \
    user_pattern="$user_pattern"     \
    max_num_lines="$max_num_lines"   \
    stop_after_max_num_lines="$stop_after_max_num_lines"   \
    timestamp_until_precise="$timestamp_until_precise"   \
    skip_n_latest="$skip_n_latest"   \
    run_awk_script_journalctl -

  codes=(${PIPESTATUS[@]})
  for status in "${codes[@]}"; do
    # The exit code 141 means SIGPIPE + 128, which is what journalctl returns
    # if awk didn't consume the whole output, which is totally normal when
    # we're querying the next page and exiting after getting enough lines.
    if [[ $status -ne 0 && $status -ne 141 ]]; then
      exit 1
    fi
  done

  echo "p:stage:$STAGE_DONE:done" 1>&2

  exit 0
fi

# A portable function to get file size.
# Usage: get_file_size /path/to/file
get_file_size() {
  case $os_kind in
    linux)
      stat -c %s "$1"
      ;;
    macos|bsd)
      stat -f %z "$1"
      ;;
    *)
      echo "error:internal error: invalid os_kind '$os_kind'" 1>&2
      return 1
  esac
}

# A portable function to get file modification time.
# Usage: get_file_modtime /path/to/file
get_file_modtime() {
  case $os_kind in
    linux)
      stat -c %y "$1"
      ;;
    macos|bsd)
      # It's not exactly equivalent of the GNU version: it doesn't print
      # fractional seconds, but good enough for our needs.
      stat -f "%SB" -t "%Y-%m-%d %H:%M:%S" "$1"
      ;;
    *)
      echo "error:internal error: invalid os_kind '$os_kind'" 1>&2
      return 1
  esac
}

logfile_prev_size=$(get_file_size $logfile_prev) || exit 1
logfile_last_size=$(get_file_size $logfile_last) || exit 1
total_size=$((logfile_prev_size+logfile_last_size)) || exit 1

if [[ "$refresh_index" == "1" ]]; then
  rm -f $indexfile || exit 1
fi

function refresh_index { # {{{
  local last_linenr=0
  local last_bytenr=0
  local prevlog_bytes=$(get_prevlog_bytenr)

  awk_vars='
    monthByName["Jan"] = "01";
    monthByName["Feb"] = "02";
    monthByName["Mar"] = "03";
    monthByName["Apr"] = "04";
    monthByName["May"] = "05";
    monthByName["Jun"] = "06";
    monthByName["Jul"] = "07";
    monthByName["Aug"] = "08";
    monthByName["Sep"] = "09";
    monthByName["Oct"] = "10";
    monthByName["Nov"] = "11";
    monthByName["Dec"] = "12";

    curYear = '${CUR_YEAR}';
    curMonth = '${CUR_MONTH}';

    yearByMonth["01"] = inferYear(1, curYear, curMonth) "";
    yearByMonth["02"] = inferYear(2, curYear, curMonth) "";
    yearByMonth["03"] = inferYear(3, curYear, curMonth) "";
    yearByMonth["04"] = inferYear(4, curYear, curMonth) "";
    yearByMonth["05"] = inferYear(5, curYear, curMonth) "";
    yearByMonth["06"] = inferYear(6, curYear, curMonth) "";
    yearByMonth["07"] = inferYear(7, curYear, curMonth) "";
    yearByMonth["08"] = inferYear(8, curYear, curMonth) "";
    yearByMonth["09"] = inferYear(9, curYear, curMonth) "";
    yearByMonth["10"] = inferYear(10, curYear, curMonth) "";
    yearByMonth["11"] = inferYear(11, curYear, curMonth) "";
    yearByMonth["12"] = inferYear(12, curYear, curMonth) "";
  '

  # Add new entries to index, if needed

  # NOTE: syslogFieldsToIndexTimestr parses the traditional systemd timestamp
  # format, like this: "Apr  5 11:07:46". But in the recent versions of
  # rsyslog, it's not the default; that traditional timestamp format can be
  # enabled by adding this line:
  #
  # $ActionFileDefaultTemplate RSYSLOG_TraditionalFileFormat
  #
  # to /etc/rsyslog.conf
  #
  # To use ISO 1806 instead (which is the default in recent rsyslog versions),
  # like "2025-04-05T11:07:46.161001+03:00":
  #
  # $ActionFileDefaultTemplate RSYSLOG_FileFormat
  #
  # But this function (and its usages) need to be updated to support it, and a
  # bunch of other time-filtering logic here. Although it's cool since it
  # includes the year, microseconds, and timezone.
  awk_functions='
function inferYear(logMonth, curYear, curMonth) {
  delta = logMonth - curMonth

  if (delta <= -11)       # log month is Jan, current is Dec -> next year
    return curYear + 1
  else if (delta >= 8)    # log month is Sep-Dec, current is Jan -> previous year
    return curYear - 1
  else
    return curYear
}

function printIndexLine(outfile, timestr, linenr, bytenr) {
  print "idx\t" timestr "\t" linenr "\t" bytenr >> outfile;
}

'$awk_func_print_percentage'
  '
# NOTE: this script MUST be executed with the "-b" awk key, which means that
# awk will work in terms of bytes, not characters. We use length($0) there and
# we rely on it being number of bytes.

  scriptInitFromLastTimestr='
    lastHHMM = substr(lastTimestr, 8, 5);
    '

  scriptSetCurTimestr='
    bytenr_cur = bytenr_next - length($0) - 1;

    month = '"$awktime_month"';
    year = '"$awktime_year"';
    day = '"$awktime_day"';
    hhmm = '"$awktime_hhmm"';

    curTimestr = year "-" month "-" day "-" hhmm;

    # Ignore decreased timestamps: treat them as if the timestamp did not change.
    if (curTimestr < lastTimestr) {
      # TODO: make sure to print that once per occurrence, and uncomment.
      # print "warn_timestamp_decreased:from " lastTimestr " to " curTimestr > "/dev/stderr"
      next;
    }
  '
  scriptSetLastTimestrEtc='
    lastTimestr = curTimestr;
    lastHHMM = curHHMM;
  '

  script1='BEGIN { bytenr_next=1; lastPercent=0 }
{
  bytenr_next += length($0)+1
  curHHMM = '"$awktime_hhmm"';
}'

  if [ -s $indexfile ]
  then
    echo "p:stage:$STAGE_INDEX_APPEND:indexing up" 1>&2

    local lastTimestr="$(tail -n 1 $indexfile | cut -f2)"
    local last_linenr="$(tail -n 1 $indexfile | cut -f3)"
    local last_bytenr="$(tail -n 1 $indexfile | cut -f4)"
    local size_to_index=$((total_size-last_bytenr))

    tail -c +$((last_bytenr-prevlog_bytes)) $logfile_last | "$awk_binary" -b "$awk_functions
  BEGIN {
    $awk_vars
    lastTimestr = \"$lastTimestr\"; $scriptInitFromLastTimestr
  }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    printIndexLine("'$indexfile'", curTimestr, NR+'$(( last_linenr-1 ))', bytenr_cur+'$(( last_bytenr-1 ))');
    printPercentage(bytenr_cur, '$size_to_index');
    '"$scriptSetLastTimestrEtc"'
  }
  ' -
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index up, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi
  else
    echo "p:stage:$STAGE_INDEX_FULL:indexing from scratch" 1>&2

    echo "prevlog_modtime	$(get_file_modtime $logfile_prev)" > $indexfile

    "$awk_binary" -b "$awk_functions BEGIN { $awk_vars lastHHMM=\"\"; }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    printIndexLine("'$indexfile'", curTimestr, NR, bytenr_cur);
    printPercentage(bytenr_cur, '$total_size');
    '"$scriptSetLastTimestrEtc"'
  }
  END { print "prevlog_lines\t" NR >> "'$indexfile'" }
  ' $logfile_prev
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index from scratch $logfile_prev, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi

  # Before we start handling $logfile_last, gotta read the last idx line (which is
  # last-but-one line) and set it for the next script, otherwise there is a gap
  # in index before the first line in the $logfile_last.
  # TODO: make sure that if there are no logs in the $lotfile1, we don't screw up.
    local lastTimestr=""
    local lastTimestrLine="$(tail -n 2 $indexfile | head -n 1)"
    if [[ "$lastTimestrLine" =~ ^idx$'\t' ]]; then
      lastTimestr="$(echo "$lastTimestrLine" | cut -f2)"
    fi
    "$awk_binary" -b "$awk_functions BEGIN { $awk_vars lastTimestr = \"$lastTimestr\"; $scriptInitFromLastTimestr }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    bytenr = bytenr_cur+'$prevlog_bytes';
    printIndexLine("'$indexfile'", curTimestr, NR+'$(get_prevlog_lines_from_index)', bytenr);
    printPercentage(bytenr, '$total_size');
    '"$scriptSetLastTimestrEtc"'
  }
  ' $logfile_last
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index from scratch $logfile_last, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi
  fi
} # }}}

# Performs index lookup by a timestr like "2006-01-02-15:04" (typically given
# as --from or --to, and it's also stored in the index in the same form).
#
# Prints result: one of "found", "before" or "after"; and if the result
# is "found", then also prints linenumber and bytenumber, space-separated.
# "before" means the given timestr is earlier than the earliest log we have,
# and "after" obviously means that it's later than the latest log we have.
#
# One possible use is:
#   read -r my_result my_linenr my_bytenr <<<$(get_linenr_and_bytenr_from_index my_timestr)
#
# Now we can use those vars $my_result, $my_linenr and $my_bytenr
function get_linenr_and_bytenr_from_index() { # {{{
  "$awk_binary" -F"\t" '
    BEGIN { isFirstIdx = 1; printed = 0; }
    $1 == "idx" {
      if ("'$1'" == $2) {
        print "found " $3 " " $4;
        printed = 1;
        exit
      } else if ("'$1'" < $2) {
        if (isFirstIdx) {
          print "before";
        } else {
          print "found " $3 " " $4;
        }
        printed = 1;
        exit
      } else {
        isFirstIdx = 0;
      }
    }
    END {
      if (!printed) {
        print "after";
      }
    }
  ' $indexfile
} # }}}

function get_prevlog_lines_from_index() { # {{{
  if ! "$awk_binary" -F"\t" 'BEGIN { found=0 } $1 == "prevlog_lines" { print $2; found = 1; exit } END { if (found == 0) { exit 1 } }' $indexfile ; then
    return 1
  fi
} # }}}

function get_prevlog_modtime_from_index() { # {{{
  if ! "$awk_binary" -F"\t" 'BEGIN { found=0 } $1 == "prevlog_modtime" { print $2; found = 1; exit } END { if (found == 0) { exit 1 } }' $indexfile ; then
    return 1
  fi
} # }}}

function get_prevlog_bytenr() { # {{{
  get_file_size $logfile_prev
} # }}}

is_outside_of_range=0
if [[ "$from" != "" || "$to" != "" ]]; then
  # If indexfile exists, check if it's valid and relevant; if not, delete it.
  if [ -e "$indexfile" ]; then
    # Check timestamp in the first line of /tmp/nerdlog_agent_index, and if
    # $logfile_prev's modification time is newer, then delete whole index
    logfile_prev_stored_modtime="$(get_prevlog_modtime_from_index)"
    logfile_prev_cur_modtile=$(get_file_modtime $logfile_prev)
    if [[ "$logfile_prev_stored_modtime" != "$logfile_prev_cur_modtile" ]]; then
      echo "debug:prev logfile $logfile_prev has changed: stored '$logfile_prev_stored_modtime', actual '$logfile_prev_cur_modtile', deleting index file" 1>&2
      rm -f $indexfile || exit 1
    fi

    if ! get_prevlog_lines_from_index > /dev/null; then
      echo "debug:broken index file (no prevlog lines), deleting it" 1>&2
      rm -f $indexfile || exit 1
    fi
  fi

  refresh_and_retry=0

  # First try to find it in index without refreshing the index

  if [ -s "$indexfile" ]; then
    if [[ "$from" != "" ]]; then
        read -r from_result from_linenr from_bytenr <<<$(get_linenr_and_bytenr_from_index "$from") || exit 1
        if [[ "$from_result" != "found" ]]; then
          echo "debug:the from ${from} isn't found, gonna refresh the index" 1>&2
          refresh_and_retry=1
        fi
    fi

    if [[ "$to" != "" ]]; then
      read -r to_result to_linenr to_bytenr <<<$(get_linenr_and_bytenr_from_index "$to") || exit 1
      if [[ "$to_result" != "found" ]]; then
        echo "debug:the to ${to} isn't found, gonna refresh the index" 1>&2
        refresh_and_retry=1
      fi
    fi
  else
    echo "debug:index file doesn't exist or is empty, gonna refresh it" 1>&2
    refresh_and_retry=1
  fi

  if [[ "$refresh_and_retry" == 1 ]]; then
    refresh_index || exit 1

    if [[ "$from" != "" ]]; then
      read -r from_result from_linenr from_bytenr <<<$(get_linenr_and_bytenr_from_index "$from") || exit 1

      if [[ "$from_result" == "before" ]]; then
        echo "debug:the from ${from} isn't found, will use the beginning" 1>&2
      elif [[ "$from_result" == "found" ]]; then
        echo "debug:the from ${from} is found: $from_linenr ($from_bytenr)" 1>&2
        if [[ "$from_bytenr" == "" || "$from_linenr" == "" ]]; then
          echo "error:from_result is found but from_bytenr and/or from_linenr is empty" 1>&2
          exit 1
        fi
      elif [[ "$from_result" == "after" ]]; then
        echo "debug:the from ${from} is after the latest log we have, will return nothing" 1>&2
        is_outside_of_range=1
      else
        echo "error:invalid from_result: $from_result" 1>&2
        exit 1
      fi
    fi

    if [[ "$to" != "" ]]; then
      read -r to_result to_linenr to_bytenr <<<$(get_linenr_and_bytenr_from_index "$to") || exit 1

      if [[ "$to_result" == "after" ]]; then
        echo "debug:the to ${to} isn't found, will use the end" 1>&2
      elif [[ "$to_result" == "found" ]]; then
        echo "debug:the to ${to} is found: $to_linenr ($to_bytenr)" 1>&2
        if [[ "$to_bytenr" == "" || "$to_linenr" == "" ]]; then
          echo "error:to_result is found but to_bytenr and/or to_linenr is empty" 1>&2
          exit 1
        fi
      elif [[ "$to_result" == "before" ]]; then
        echo "debug:the to ${to} is before the first log we have, will return nothing" 1>&2
        is_outside_of_range=1
      else
        echo "error:invalid to_result: $to_result" 1>&2
        exit 1
      fi
    fi

  fi
else
  if ! [ -s $indexfile ]; then
    echo "debug:neither --from or --to are given, but index doesn't exist at all, gonna rebuild" 1>&2
    refresh_index || exit 1
  fi
fi

if [[ $is_outside_of_range == 1 ]]; then
  echo "p:stage:$STAGE_DONE:done" 1>&2
  exit 0
fi

echo "p:stage:$STAGE_QUERYING:querying logs" 1>&2

prevlog_lines=$(get_prevlog_lines_from_index)
prevlog_bytes=$(get_prevlog_bytenr)

from_linenr_int=$from_linenr
if [[ "$from_linenr" == "" ]]; then
  from_linenr_int=1
fi

lines_until_check=''
if [[ "$lines_until" != "" ]]; then
  lines_until_check="if (NR >= $((lines_until-from_linenr_int+1))) { next; }"
fi

num_bytes_to_scan=0
if [[ "$from_bytenr" == "" && "$to_bytenr" == "" ]]; then
  # Getting _all_ available logs
  num_bytes_to_scan=$total_size
elif [[ "$from_bytenr" != "" && "$to_bytenr" == "" ]]; then
  # Getting logs from some point in time to the very end (most frequent case)
  num_bytes_to_scan=$((total_size-from_bytenr))
elif [[ "$from_bytenr" == "" && "$to_bytenr" != "" ]]; then
  # Getting logs from the beginning until some point in time
  num_bytes_to_scan=$((to_bytenr))
else
  # Getting logs between two points T1 and T2
  num_bytes_to_scan=$((to_bytenr-from_bytenr))
fi


# NOTE: there are multiple ways to tail a file, and performance differs greatly:
# Log file has 21789347 lines:
#
#ubuntu@dummy-node-01:~$ time cat /var/log/syslog.1 | tail -n +16789340 > /dev/null

#real    0m4.523s
#user    0m0.869s
#sys     0m6.915s
#ubuntu@dummy-node-01:~$ time tail -n +16789340 /var/log/syslog.1 > /dev/null

#real    0m2.184s
#user    0m0.660s
#sys     0m1.524s
#ubuntu@dummy-node-01:~$ time tail -n 5000000 /var/log/syslog.1 > /dev/null

#real    0m1.260s
#user    0m0.412s
#sys     0m0.848s

# So it's best to tail file directly (without cat) and also whenever possible
# do the "-n N", not "-n +N" (but for the latest logfile, which is constantly
# appended to, we have to use the "-n +N")

# Generate commands to get all the logs as per requested timerange.
declare -a cmds
if [[ "$from_bytenr" != "" && $(( from_bytenr > prevlog_bytes )) == 1 ]]; then
  # Only $logfile_last is used.
  from_bytenr=$(( from_bytenr - prevlog_bytes ))
  if [[ "$to_bytenr" != "" ]]; then
    to_bytenr=$(( to_bytenr - prevlog_bytes ))
    echo "debug:Getting logs from offset $from_bytenr, only $((to_bytenr - from_bytenr)) bytes, all in the latest $logfile_last" 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_last | head -c $((to_bytenr - from_bytenr))")
  else
    # Most common case
    echo "debug:Getting logs from offset $from_bytenr until the end of latest $logfile_last." 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_last")
  fi
elif [[ "$to_bytenr" != "" && $(( to_bytenr <= prevlog_bytes )) == 1 ]]; then
  # Only $logfile_prev is used.
  if [[ "$from_bytenr" != "" ]]; then
    echo "debug:Getting logs from offset $from_bytenr, only $((to_bytenr - from_bytenr)) bytes, all in the prev $logfile_prev" 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_prev | head -c $((to_bytenr - from_bytenr))")
  else
    echo "debug:Getting logs from the very beginning to offset $(( to_bytenr - 1 )), all in the prev $logfile_prev." 1>&2
    cmds+=("head -c $(( to_bytenr - 1)) $logfile_prev")
  fi
else
  # Both log files are used
  if [[ "$from_bytenr" != "" ]]; then
    info="Getting logs from offset $from_bytenr in prev $logfile_prev"
    cmds+=("tail -c +$from_bytenr $logfile_prev")
  else
    info="Getting logs from the very beginning in prev $logfile_prev"
    cmds+=("cat $logfile_prev")
  fi

  if [[ "$to_bytenr" != "" ]]; then
    info="$info to offset $(( to_bytenr - prevlog_bytes - 1 )) in latest $logfile_last"
    cmds+=("head -c $(( to_bytenr - prevlog_bytes - 1 )) $logfile_last")
  else
    info="$info until the end of latest $logfile_last"
    cmds+=("cat $logfile_last")
  fi

  echo "debug:$info" 1>&2
fi

cmds_concatenated="$(concat_cmds_array)"
echo "debug:Command to filter logs by time range:" 1>&2
echo "debug: bash -c '$cmds_concatenated'" 1>&2

# Now execute all those commands, and feed those logs to the awk script
# which will analyze them and produce the final output.
eval $cmds_concatenated | \
  user_pattern="$user_pattern"                          \
  max_num_lines="$max_num_lines"                        \
  num_bytes_to_scan="$num_bytes_to_scan"                \
  lines_until_check="$lines_until_check"                \
  prevlog_lines="$prevlog_lines"                        \
  from_linenr_int="$from_linenr_int"                    \
  run_awk_script_logfiles -

codes=(${PIPESTATUS[@]})
for status in "${codes[@]}"; do
  if [[ $status -ne 0 ]]; then
    exit 1
  fi
done

echo "p:stage:$STAGE_DONE:done" 1>&2
