import { storiesOf } from '@storybook/react'
import isChromatic from 'chromatic/isChromatic'
import { createMemoryHistory } from 'history'
import React from 'react'

import { NOOP_TELEMETRY_SERVICE } from '@sourcegraph/shared/src/telemetry/telemetryService'

import { WebStory } from '../../../../../components/WebStory'
import { LINE_CHART_CONTENT_MOCK } from '../../../../mocks/charts-content'

import { ChartViewContent } from './ChartViewContent'
import styles from './ChartViewContent.story.module.scss'

const history = createMemoryHistory()

const commonProps = {
    history,
    animate: !isChromatic(),
    viewID: '1',
    telemetryService: NOOP_TELEMETRY_SERVICE,
    className: styles.chart,
}

const { add } = storiesOf('web/ChartViewContent', module).addDecorator(story => (
    <WebStory>{() => <div className={styles.charts}>{story()}</div>}</WebStory>
))

const DATA_WITH_STEP = [
    { dateTime: 1604188800000, series0: 3725 },
    {
        dateTime: 1606780800000,
        series0: 3725,
    },
    { dateTime: 1609459200000, series0: 3725 },
    {
        dateTime: 1612137600000,
        series0: 3725,
    },
    { dateTime: 1614556800000, series0: 3725 },
    {
        dateTime: 1617235200000,
        series0: 3725,
    },
    { dateTime: 1619827200000, series0: 3728 },
    {
        dateTime: 1622505600000,
        series0: 3827,
    },
    { dateTime: 1625097600000, series0: 3827 },
    {
        dateTime: 1627776000000,
        series0: 3827,
    },
    { dateTime: 1630458631000, series0: 3053 },
    {
        dateTime: 1633452311000,
        series0: 3053,
    },
    { dateTime: 1634952495000, series0: 3053 },
]

add('Line chart', () => (
    <>
        <ChartViewContent {...commonProps} content={LINE_CHART_CONTENT_MOCK} />
        <ChartViewContent
            {...commonProps}
            content={{
                chart: 'line',
                data: DATA_WITH_STEP,
                series: [
                    {
                        dataKey: 'series0',
                        name: 'Series 0',
                        stroke: 'var(--blue)',
                    },
                ],
                xAxis: {
                    dataKey: 'dateTime',
                    scale: 'time',
                    type: 'number',
                },
            }}
        />
    </>
))

add('Line chart with missing data', () => (
    <ChartViewContent
        {...commonProps}
        content={{
            chart: 'line',
            data: [
                { x: 1588965700286 - 4 * 24 * 60 * 60 * 1000, a: null, b: null },
                { x: 1588965700286 - 3 * 24 * 60 * 60 * 1000, a: null, b: null },
                { x: 1588965700286 - 2 * 24 * 60 * 60 * 1000, a: 94, b: 200 },
                { x: 1588965700286 - 1.5 * 24 * 60 * 60 * 1000, a: 134, b: null },
                { x: 1588965700286 - 1.3 * 24 * 60 * 60 * 1000, a: null, b: 150 },
                { x: 1588965700286 - 1 * 24 * 60 * 60 * 1000, a: 134, b: 190 },
                { x: 1588965700286, a: 123, b: 170 },
            ],
            series: [
                {
                    dataKey: 'a',
                    name: 'A metric',
                    stroke: 'var(--blue)',
                    linkURLs: [
                        '#A:1st_data_point',
                        '#A:2nd_data_point',
                        '#A:3rd_data_point',
                        '#A:4th_data_point',
                        '#A:5th_data_point',
                    ],
                },
                {
                    dataKey: 'b',
                    name: 'B metric',
                    stroke: 'var(--warning)',
                    linkURLs: [
                        '#B:1st_data_point',
                        '#B:2nd_data_point',
                        '#B:3rd_data_point',
                        '#B:4th_data_point',
                        '#B:5th_data_point',
                    ],
                },
            ],
            xAxis: {
                dataKey: 'x',
                scale: 'time',
                type: 'number',
            },
        }}
    />
))

add('Line chart with 0 to 1 data', () => (
    <ChartViewContent
        {...commonProps}
        content={{
            chart: 'line',
            data: [
                { x: 1588965700286 - 4 * 24 * 60 * 60 * 1000, a: 0 },
                { x: 1588965700286 - 2 * 24 * 60 * 60 * 1000, a: 1 },
            ],
            series: [
                {
                    dataKey: 'a',
                    name: 'A metric',
                    stroke: 'var(--red)',
                },
            ],
            xAxis: {
                dataKey: 'x',
                scale: 'time',
                type: 'number',
            },
        }}
    />
))

add('Bar chart', () => (
    <ChartViewContent
        {...commonProps}
        content={{
            chart: 'bar',
            data: [
                { name: 'A', value: 183 },
                { name: 'B', value: 145 },
                { name: 'C', value: 94 },
                { name: 'D', value: 134 },
                { name: 'E', value: 123 },
            ],
            series: [
                {
                    dataKey: 'value',
                    name: 'A metric',
                    fill: 'var(--oc-teal-7)',
                    linkURLs: [
                        '#1st_data_point',
                        '#2nd_data_point',
                        '#3rd_data_point',
                        '#4th_data_point',
                        '#5th_data_point',
                    ],
                },
            ],
            xAxis: {
                dataKey: 'name',
                type: 'category',
            },
        }}
    />
))

add('Pie chart', () => (
    <ChartViewContent
        {...commonProps}
        content={{
            chart: 'pie',
            pies: [
                {
                    dataKey: 'value',
                    nameKey: 'name',
                    fillKey: 'fill',
                    linkURLKey: 'linkURL',
                    data: [
                        {
                            name: 'Covered',
                            value: 0.3,
                            fill: 'var(--success)',
                            linkURL: '#Covered',
                        },
                        {
                            name: 'Not covered',
                            value: 0.7,
                            fill: 'var(--danger)',
                            linkURL: '#Not_covered',
                        },
                    ],
                },
            ],
        }}
    />
))
